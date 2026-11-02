package api

import (
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
)

type webVNCTicket struct {
	ContainerName string
	ContainerUUID string
	Username      string
	SubUser       bool
	ClientIP      string
	UserAgent     string
	ExpiresAt     time.Time
}

var webVNCTickets = struct {
	sync.Mutex
	items map[string]webVNCTicket
}{items: map[string]webVNCTicket{}}

func HandleVNCTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	if !requireScope(w, r, "terminal:vnc") {
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
	if !c.IsKVM() {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "VNC console is only available for KVM VMs"})
		return
	}

	username, isSubUser := vncRequesterIdentity(r)
	ticket := randomHex(32)
	webVNCTickets.Lock()
	cleanupExpiredWebVNCTicketsLocked(time.Now())
	webVNCTickets.items[ticket] = webVNCTicket{
		ContainerName: c.Name,
		ContainerUUID: c.UUID,
		Username:      username,
		SubUser:       isSubUser,
		ClientIP:      clientIP(r),
		UserAgent:     r.UserAgent(),
		ExpiresAt:     time.Now().Add(60 * time.Second),
	}
	webVNCTickets.Unlock()

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"ticket": ticket},
	})
}

// HandleVNCProxy proxies a KVM VM's local libvirt VNC socket to the browser.
func HandleVNCProxy(w http.ResponseWriter, r *http.Request) {
	ticket := webVNCTicketFromRequest(r)
	if ticket == "" {
		http.Error(w, "ticket required", http.StatusUnauthorized)
		return
	}

	containerName := r.URL.Query().Get("container")
	if containerName == "" {
		http.Error(w, "container name required", http.StatusBadRequest)
		return
	}

	item, ok := consumeWebVNCTicket(ticket, containerName, r)
	if !ok {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}

	c := config.FindContainerByName(containerName)
	if c == nil || c.UUID != item.ContainerUUID {
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}
	if !c.IsKVM() {
		http.Error(w, "VNC console is only available for KVM VMs", http.StatusBadRequest)
		return
	}
	if c.Status != "running" {
		http.Error(w, "container is not running", http.StatusBadRequest)
		return
	}

	vncPort, err := kvmManager.RefreshVNCPort(c.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("VNC display is not available: %v", err), http.StatusBadRequest)
		return
	}

	vncConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", vncPort)), 5*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("VNC connection failed: %v", err), http.StatusBadRequest)
		return
	}
	defer vncConn.Close()

	responseHeader := http.Header{}
	if protocol := webVNCResponseProtocol(r); protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Printf("WebVNC upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("WebVNC connected for container %s as %s (sub_user=%t) -> 127.0.0.1:%d", containerName, item.Username, item.SubUser, vncPort)

	done := make(chan string, 2)
	var writeMu sync.Mutex
	go streamVNCToWebSocket(ws, &writeMu, vncConn, done)
	go streamWebSocketToVNC(ws, vncConn, done)

	reason := <-done
	_ = vncConn.Close()
	_ = ws.Close()
	log.Printf("WebVNC disconnected for container %s as %s: %s", containerName, item.Username, reason)
}

func vncRequesterIdentity(r *http.Request) (string, bool) {
	if ctx, ok := authContextFromRequest(r); ok {
		switch ctx.Type {
		case authTypeSubUser:
			return ctx.Username, true
		case authTypeAPIKey:
			return ctx.Actor, false
		case authTypeAdmin:
			return ctx.Username, false
		}
	}
	claims, ok := claimsFromRequest(r)
	if !ok {
		return "api-key", false
	}
	if subUser, ok := claims["sub_user"].(string); ok && subUser != "" {
		return subUser, true
	}
	if username, ok := claims["username"].(string); ok && username != "" {
		return username, false
	}
	return "unknown", false
}

func webVNCTicketFromRequest(r *http.Request) string {
	for _, protocol := range websocket.Subprotocols(r) {
		const prefix = "clicd-vnc-ticket."
		if len(protocol) > len(prefix) && protocol[:len(prefix)] == prefix {
			return protocol[len(prefix):]
		}
	}
	return r.URL.Query().Get("ticket")
}

func webVNCResponseProtocol(r *http.Request) string {
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == "binary" {
			return protocol
		}
	}
	for _, protocol := range websocket.Subprotocols(r) {
		const prefix = "clicd-vnc-ticket."
		if len(protocol) > len(prefix) && protocol[:len(prefix)] == prefix {
			return protocol
		}
	}
	return ""
}

func consumeWebVNCTicket(ticket, containerName string, r *http.Request) (webVNCTicket, bool) {
	now := time.Now()
	webVNCTickets.Lock()
	defer webVNCTickets.Unlock()
	cleanupExpiredWebVNCTicketsLocked(now)
	item, ok := webVNCTickets.items[ticket]
	if !ok {
		return webVNCTicket{}, false
	}
	delete(webVNCTickets.items, ticket)
	return item, item.ContainerName == containerName &&
		item.ClientIP == clientIP(r) &&
		item.UserAgent == r.UserAgent() &&
		now.Before(item.ExpiresAt)
}

func cleanupExpiredWebVNCTicketsLocked(now time.Time) {
	for ticket, item := range webVNCTickets.items {
		if !now.Before(item.ExpiresAt) {
			delete(webVNCTickets.items, ticket)
		}
	}
}

func streamVNCToWebSocket(ws *websocket.Conn, writeMu *sync.Mutex, src io.Reader, done chan<- string) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			writeMu.Lock()
			writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			writeMu.Unlock()
			if writeErr != nil {
				done <- fmt.Sprintf("browser websocket write failed: %v", writeErr)
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				done <- "VNC server closed connection"
			} else {
				done <- fmt.Sprintf("VNC server read failed: %v", err)
			}
			return
		}
	}
}

func streamWebSocketToVNC(ws *websocket.Conn, dst net.Conn, done chan<- string) {
	for {
		messageType, msg, err := ws.ReadMessage()
		if err != nil {
			done <- fmt.Sprintf("browser websocket read failed: %v", err)
			return
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}
		if _, err := dst.Write(msg); err != nil {
			done <- fmt.Sprintf("VNC server write failed: %v", err)
			return
		}
	}
}
