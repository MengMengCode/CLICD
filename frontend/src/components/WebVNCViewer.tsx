import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { Button } from './ui/button'
import { useEffect, useRef, useState } from 'react'
import { Monitor, RefreshCw, Send, X } from 'lucide-react'
import RFBModule from '@novnc/novnc/lib/rfb'
import { createVNCTicket, getWebVNCUrl } from '../services/api'

type RFBConstructor = new (
  target: HTMLElement,
  url: string,
  options?: { credentials?: Record<string, string>; shared?: boolean; repeaterID?: string; wsProtocols?: string[] }
) => RFBInstance

interface RFBInstance extends EventTarget {
  scaleViewport: boolean
  resizeSession: boolean
  focusOnClick: boolean
  viewOnly: boolean
  qualityLevel: number
  compressionLevel: number
  background: string
  disconnect(): void
  sendCtrlAltDel(): void
}

const RFB = resolveRFBConstructor(RFBModule)

function resolveRFBConstructor(moduleValue: unknown): RFBConstructor {
  if (typeof moduleValue === 'function') {
    return moduleValue as RFBConstructor
  }
  const maybeDefault = (moduleValue as { default?: unknown })?.default
  if (typeof maybeDefault === 'function') {
    return maybeDefault as RFBConstructor
  }
  throw new Error('noVNC RFB constructor is unavailable')
}

interface WebVNCViewerProps {
  containerName: string
  onClose: () => void
}

export default function WebVNCViewer({ containerName, onClose }: WebVNCViewerProps) {
  const screenRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<RFBInstance | null>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting')
  const [errorMsg, setErrorMsg] = useState('')

  const cleanup = () => {
    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }
  }

  const ensureResizeObserver = () => {
    if ('ResizeObserver' in window) return

    class FallbackResizeObserver {
      private target: Element | null = null
      private timer = 0
      private lastWidth = -1
      private lastHeight = -1

      constructor(private callback: ResizeObserverCallback) {}

      observe = (target: Element) => {
        this.target = target
        this.check()
        this.timer = window.setInterval(this.check, 250)
        window.addEventListener('resize', this.check)
      }

      unobserve = () => this.disconnect()

      disconnect = () => {
        if (this.timer) window.clearInterval(this.timer)
        this.timer = 0
        window.removeEventListener('resize', this.check)
        this.target = null
      }

      private check = () => {
        if (!this.target) return
        const contentRect = this.target.getBoundingClientRect()
        if (contentRect.width === this.lastWidth && contentRect.height === this.lastHeight) return
        this.lastWidth = contentRect.width
        this.lastHeight = contentRect.height
        this.callback([{ target: this.target, contentRect } as ResizeObserverEntry], this as unknown as ResizeObserver)
      }
    }

    ;(window as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver = FallbackResizeObserver as unknown as typeof ResizeObserver
  }

  const connect = async () => {
    const target = screenRef.current
    if (!target) return

    cleanup()
    target.innerHTML = ''
    setStatus('connecting')
    setErrorMsg('')

    let ticket = ''
    try {
      const response = await createVNCTicket(containerName)
      ticket = response.data.data?.ticket || ''
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      setStatus('error')
      setErrorMsg(error.response?.data?.message || 'WebVNC ticket 创建失败，请重新登录后再试')
      return
    }

    if (!ticket) {
      setStatus('error')
      setErrorMsg('WebVNC ticket 为空，请重新登录后再试')
      return
    }

    try {
      ensureResizeObserver()
      const rfb = new RFB(target, getWebVNCUrl(containerName), {
        wsProtocols: ['binary', `clicd-vnc-ticket.${ticket}`],
      })
      rfb.scaleViewport = true
      rfb.resizeSession = false
      rfb.focusOnClick = true
      rfb.qualityLevel = 6
      rfb.compressionLevel = 2
      rfb.background = '#050505'
      rfb.addEventListener('connect', () => setStatus('connected'))
      rfb.addEventListener('disconnect', (event) => {
        const detail = (event as CustomEvent<{ clean?: boolean }>).detail
        setStatus((current) => current === 'error' ? current : 'disconnected')
        if (detail && detail.clean === false) {
          setErrorMsg('WebVNC 连接已断开，请确认虚拟机正在运行且 VNC 控制台可用')
        }
      })
      rfb.addEventListener('securityfailure', () => {
        setStatus('error')
        setErrorMsg('VNC 安全协商失败')
      })
      rfb.addEventListener('credentialsrequired', () => {
        setStatus('error')
        setErrorMsg('当前 VNC 控制台要求密码，暂不支持自动输入')
      })
      rfbRef.current = rfb
    } catch (err) {
      console.error(err)
      setStatus('error')
      const message = err instanceof Error && err.message ? `：${err.message}` : ''
      setErrorMsg(`WebVNC 初始化失败${message}`)
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(connect, 100)
    return () => {
      window.clearTimeout(timer)
      cleanup()
    }
  }, [containerName])

  return (
    <Card className="flex h-full flex-col overflow-hidden">
      <div className="flex shrink-0 items-center justify-between border-b border-border bg-muted px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Monitor className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-medium text-foreground">WebVNC - {containerName}</span>
          {status === 'connected' && <Badge variant="secondary" className="px-1.5 py-0.5 text-xs">已连接</Badge>}
          {status === 'connecting' && <Badge variant="secondary" className="px-1.5 py-0.5 text-xs">连接中...</Badge>}
          {status === 'disconnected' && <Badge variant="secondary" className="px-1.5 py-0.5 text-xs">已断开</Badge>}
          {status === 'error' && <Badge variant="destructive" className="px-1.5 py-0.5 text-xs">连接失败</Badge>}
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" onClick={() => rfbRef.current?.sendCtrlAltDel()} className="h-auto inline-flex items-center gap-1 px-2 py-1.5 text-xs" title="发送 Ctrl+Alt+Del">
            <Send className="h-3.5 w-3.5" />
            Ctrl+Alt+Del
          </Button>
          <Button variant="ghost" onClick={connect} className="h-auto p-1.5 text-xs" title="重新连接">
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" onClick={onClose} className="h-auto p-1.5" title="关闭">
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1 overflow-hidden bg-black">
        <div ref={screenRef} className="h-full w-full [&>div]:h-full [&>div]:w-full [&_canvas]:block" />
        {(status === 'connecting' || status === 'error' || (status === 'disconnected' && errorMsg)) && (
          <div className={`absolute inset-x-0 bottom-0 border-t px-4 py-2 text-sm  ${status === "error" ? "border-red-900 bg-red-950 text-red-100" : "border-border bg-gray-950 text-muted-foreground"}`}>
            {status === 'connecting' ? '正在连接 KVM VNC 控制台...' : (errorMsg || 'WebVNC 已断开')}
          </div>
        )}
      </div>
    </Card>
  )
}
