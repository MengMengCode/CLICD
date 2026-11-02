package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"clicd/internal/config"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type terminalResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type webSSHTicket struct {
	ContainerName string
	ExpiresAt     time.Time
}

var webSSHTickets = struct {
	sync.Mutex
	items map[string]webSSHTicket
}{items: map[string]webSSHTicket{}}

func HandleWebSSHTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	if !requireScope(w, r, "terminal:ssh") {
		return
	}
	var req struct {
		ContainerName string `json:"container_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContainerName == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Container name required"})
		return
	}
	if !isContainerAllowedForRequest(r, req.ContainerName) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
		return
	}
	c := config.FindContainerByName(req.ContainerName)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}

	ticket := randomHex(32)
	webSSHTickets.Lock()
	cleanupExpiredWebSSHTicketsLocked(time.Now())
	webSSHTickets.items[ticket] = webSSHTicket{
		ContainerName: req.ContainerName,
		ExpiresAt:     time.Now().Add(60 * time.Second),
	}
	webSSHTickets.Unlock()

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"ticket": ticket},
	})
}

// HandleWebSSH proxies an SSH session to the browser over WebSocket.
func HandleWebSSH(w http.ResponseWriter, r *http.Request) {
	ticket := webSSHTicketFromRequest(r)
	if ticket == "" {
		http.Error(w, "ticket required", http.StatusUnauthorized)
		return
	}

	containerName := r.URL.Query().Get("container")
	if containerName == "" {
		http.Error(w, "container name required", http.StatusBadRequest)
		return
	}

	_, ok := consumeWebSSHTicket(ticket, containerName)
	if !ok {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}

	c := config.FindContainerByName(containerName)
	if c == nil {
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}
	if c.Status != "running" {
		http.Error(w, "container is not running", http.StatusBadRequest)
		return
	}
	if c.IP == "" {
		var ip string
		var err error
		if c.IsKVM() {
			ip, err = kvmManager.GetContainerIP(c.VirshName())
		} else {
			ip, err = lxcManager.GetContainerIP(c.LxcName())
		}
		if err == nil {
			c.IP = ip
			config.SaveConfig()
		}
	}
	if c.IP == "" && !c.IsKVM() {
		if ip, err := lxcManager.EnsureContainerIPv4(c.ID); err == nil && ip != "" {
			c.IP = ip
		}
	}
	if c.IP == "" && c.IsKVM() {
		if err := kvmManager.EnsureSSH(c.ID); err == nil {
			if refreshed := config.FindContainer(c.ID); refreshed != nil {
				c = refreshed
			}
		}
	}
	if c.IP == "" {
		http.Error(w, "container ip is not available", http.StatusBadRequest)
		return
	}
	responseHeader := http.Header{}
	if protocol := webSSHTicketProtocol(r); protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Printf("WebSSH upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	if c.SSHPassword == "" {
		if c.IsKVM() {
			writeWebSocketText(ws, nil, "\r\nKVM SSH password is not available. Reinstall or reset after SSH is ready.\r\n")
			return
		}
		writeWebSocketText(ws, nil, "\r\nPreparing SSH service. This can take up to 90 seconds on first boot...\r\n")
		if err := lxcManager.EnsureSSH(c.ID); err != nil {
			writeWebSocketText(ws, nil, fmt.Sprintf("\r\nSSH auto setup failed: %v\r\n", err))
			return
		}
		if refreshed := config.FindContainer(c.ID); refreshed != nil {
			c = refreshed
		}
	}
	if c.SSHPassword == "" {
		writeWebSocketText(ws, nil, "\r\nSSH password is empty after auto setup\r\n")
		return
	}

	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(c.SSHPassword),
		},
		HostKeyCallback: containerHostKeyCallback(c),
		Timeout:         4 * time.Second,
	}

	addr := net.JoinHostPort(c.IP, "22")
	writeWebSocketText(ws, nil, fmt.Sprintf("Connecting to %s...\r\n", addr))
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		if c.IsKVM() {
			writeWebSocketText(ws, nil, "\r\nSSH is not ready yet, preparing KVM guest service. This can take a few minutes on first boot...\r\n")
			if setupErr := kvmManager.EnsureSSH(c.ID); setupErr != nil {
				writeWebSocketText(ws, nil, fmt.Sprintf("\r\nKVM SSH auto setup failed: %v\r\n", setupErr))
				return
			}
			if refreshed := config.FindContainer(c.ID); refreshed != nil {
				c = refreshed
			}
			if ip, ipErr := kvmManager.GetContainerIP(c.VirshName()); ipErr == nil && ip != "" {
				c.IP = ip
				config.SaveConfig()
				addr = net.JoinHostPort(c.IP, "22")
			}
			sshConfig.Auth = []ssh.AuthMethod{ssh.Password(c.SSHPassword)}
			sshConfig.Timeout = 10 * time.Second
			client, err = ssh.Dial("tcp", addr, sshConfig)
			if err != nil {
				writeWebSocketText(ws, nil, fmt.Sprintf("\r\nWebSSH connection failed: %v\r\n", err))
				return
			}
		} else {
			writeWebSocketText(ws, nil, "\r\nSSH is not ready yet, preparing service. This can take up to 90 seconds on first boot...\r\n")
			if setupErr := lxcManager.EnsureSSH(c.ID); setupErr != nil {
				writeWebSocketText(ws, nil, fmt.Sprintf("\r\nSSH auto setup failed: %v\r\n", setupErr))
				return
			}
			if refreshed := config.FindContainer(c.ID); refreshed != nil {
				c = refreshed
			}
			if ip, ipErr := lxcManager.GetContainerIP(c.LxcName()); ipErr == nil && ip != "" {
				c.IP = ip
				config.SaveConfig()
				addr = net.JoinHostPort(c.IP, "22")
			}
			sshConfig.Auth = []ssh.AuthMethod{ssh.Password(c.SSHPassword)}
			sshConfig.Timeout = 10 * time.Second
			client, err = ssh.Dial("tcp", addr, sshConfig)
			if err != nil {
				writeWebSocketText(ws, nil, fmt.Sprintf("\r\nWebSSH connection failed: %v\r\n", err))
				return
			}
		}
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to create SSH session: %v\r\n", err))
		return
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to open SSH stdin: %v\r\n", err))
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to open SSH stdout: %v\r\n", err))
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to open SSH stderr: %v\r\n", err))
		return
	}

	if err := session.RequestPty("xterm-256color", 40, 120, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to request pty: %v\r\n", err))
		return
	}

	if err := session.Shell(); err != nil {
		writeWebSocketText(ws, nil, fmt.Sprintf("\r\nFailed to start shell: %v\r\n", err))
		return
	}
	writeWebSocketText(ws, nil, "\r\nSSH shell ready. Press Enter if the prompt is not visible.\r\n")
	_, _ = stdin.Write([]byte("\n"))

	log.Printf("WebSSH connected for container %s -> %s", containerName, addr)

	done := make(chan struct{}, 3)
	var writeMu sync.Mutex

	go streamSSHOutput(ws, &writeMu, stdout, done)
	go streamSSHOutput(ws, &writeMu, stderr, done)

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			messageType, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			if messageType == websocket.TextMessage {
				var resize terminalResizeMessage
				if err := json.Unmarshal(msg, &resize); err == nil && resize.Type == "resize" {
					if resize.Rows > 0 && resize.Cols > 0 {
						_ = session.WindowChange(resize.Rows, resize.Cols)
					}
					continue
				}
			}

			if _, err := stdin.Write(msg); err != nil {
				return
			}
		}
	}()

	<-done
	_ = session.Signal(ssh.SIGTERM)
	log.Printf("WebSSH disconnected for container %s", containerName)
}

func containerHostKeyCallback(c *config.Container) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		sum := sha256.Sum256(key.Marshal())
		fingerprint := hex.EncodeToString(sum[:])
		if c.SSHHostKey != "" && c.SSHHostKey != fingerprint {
			return fmt.Errorf("container SSH host key mismatch")
		}
		if c.SSHHostKey == "" {
			c.SSHHostKey = fingerprint
			config.SaveConfig()
		}
		return nil
	}
}

func webSSHTicketFromRequest(r *http.Request) string {
	for _, protocol := range websocket.Subprotocols(r) {
		const prefix = "clicd-ticket."
		if len(protocol) > len(prefix) && protocol[:len(prefix)] == prefix {
			return protocol[len(prefix):]
		}
	}
	return ""
}

func webSSHTicketProtocol(r *http.Request) string {
	for _, protocol := range websocket.Subprotocols(r) {
		const prefix = "clicd-ticket."
		if len(protocol) > len(prefix) && protocol[:len(prefix)] == prefix {
			return protocol
		}
	}
	return ""
}

func streamSSHOutput(ws *websocket.Conn, writeMu *sync.Mutex, src io.Reader, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	buf := make([]byte, 8192)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			writeMu.Lock()
			writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			writeMu.Unlock()
			if writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func writeWebSocketText(ws *websocket.Conn, writeMu *sync.Mutex, msg string) {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	_ = ws.WriteMessage(websocket.TextMessage, []byte(msg))
}

func consumeWebSSHTicket(ticket, containerName string) (webSSHTicket, bool) {
	now := time.Now()
	webSSHTickets.Lock()
	defer webSSHTickets.Unlock()
	cleanupExpiredWebSSHTicketsLocked(now)
	item, ok := webSSHTickets.items[ticket]
	if !ok {
		return webSSHTicket{}, false
	}
	delete(webSSHTickets.items, ticket)
	return item, item.ContainerName == containerName && now.Before(item.ExpiresAt)
}

func cleanupExpiredWebSSHTicketsLocked(now time.Time) {
	for ticket, item := range webSSHTickets.items {
		if !now.Before(item.ExpiresAt) {
			delete(webSSHTickets.items, ticket)
		}
	}
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
