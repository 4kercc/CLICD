import { useEffect, useRef, useState } from 'react'
import {
  Check,
  Clipboard,
  Copy,
  CornerDownLeft,
  Download,
  Keyboard,
  Maximize2,
  Minimize2,
  Monitor,
  Play,
  RefreshCw,
  Send,
  Sparkles,
  Square,
  Trash2,
  Upload,
  X,
  Zap,
} from 'lucide-react'
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
  showDotCursor: boolean
  clipViewport: boolean
  qualityLevel: number
  compressionLevel: number
  background: string
  disconnect(): void
  sendCtrlAltDel(): void
  clipboardPasteFrom(text: string): void
  sendKey(keysym: number, code?: string, down?: boolean): void
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

// charToKeysym maps any character to its standard X11 keysym
function charToKeysym(ch: string): number {
  if (ch === '\r' || ch === '\n') return 0xff0d // XK_Return
  if (ch === '\t') return 0xff09 // XK_Tab
  if (ch === '\b') return 0xff08 // XK_BackSpace
  if (ch === '\x1b') return 0xff1b // XK_Escape

  const cp = ch.codePointAt(0)
  if (cp === undefined) return 0

  // Standard ASCII and Latin-1 (0x20..0xff)
  if (cp >= 0x20 && cp <= 0xff) {
    return cp
  }

  // Unicode character keysym extension (ISO 10646)
  if (cp > 0xff) {
    return 0x01000000 | cp
  }

  return cp
}

interface WebVNCViewerProps {
  containerName: string
  onClose: () => void
}

type VNCMode = 'smooth' | 'clear'

export default function WebVNCViewer({ containerName, onClose }: WebVNCViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const screenRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<RFBInstance | null>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting')
  const [errorMsg, setErrorMsg] = useState('')
  const [mode, setMode] = useState<VNCMode>('smooth')
  const [isFullscreen, setIsFullscreen] = useState(false)

  // Clipboard & Typing tools state
  const [showClipboardDrawer, setShowClipboardDrawer] = useState(false)
  const [clipboardText, setClipboardText] = useState('')
  const [guestClipboardText, setGuestClipboardText] = useState('')
  const [isTyping, setIsTyping] = useState(false)
  const [typeProgress, setTypeProgress] = useState<{ current: number; total: number } | null>(null)
  const [copySuccess, setCopySuccess] = useState(false)
  const [pasteSuccess, setPasteSuccess] = useState(false)
  const stopTypingRef = useRef(false)

  const cleanup = () => {
    stopTypingRef.current = true
    setIsTyping(false)
    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }
  }

  const applyModeSettings = (rfb: RFBInstance, targetMode: VNCMode) => {
    if (targetMode === 'smooth') {
      rfb.qualityLevel = 2
      rfb.compressionLevel = 8
    } else {
      rfb.qualityLevel = 7
      rfb.compressionLevel = 4
    }
  }

  const switchMode = (newMode: VNCMode) => {
    setMode(newMode)
    if (rfbRef.current) {
      applyModeSettings(rfbRef.current, newMode)
    }
  }

  const toggleFullscreen = async () => {
    const el = containerRef.current
    if (!el) return
    if (!document.fullscreenElement) {
      try {
        await el.requestFullscreen()
        setIsFullscreen(true)
      } catch { /* ignored */ }
    } else {
      try {
        await document.exitFullscreen()
        setIsFullscreen(false)
      } catch { /* ignored */ }
    }
  }

  useEffect(() => {
    const handleFSChange = () => setIsFullscreen(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', handleFSChange)
    return () => document.removeEventListener('fullscreenchange', handleFSChange)
  }, [])

  // Send clipboard to VNC server (RFB ClientCutText)
  const handleSendVNCClipboard = () => {
    if (!rfbRef.current || !clipboardText) return
    try {
      rfbRef.current.clipboardPasteFrom(clipboardText)
      setPasteSuccess(true)
      setTimeout(() => setPasteSuccess(false), 2000)
    } catch (e) {
      console.error('Failed to send clipboard to VNC:', e)
    }
  }

  // Read local host clipboard into text area
  const handleReadHostClipboard = async () => {
    try {
      if (navigator.clipboard && navigator.clipboard.readText) {
        const text = await navigator.clipboard.readText()
        if (text) {
          setClipboardText(text)
        }
      }
    } catch (e) {
      console.warn('Cannot read host clipboard automatically:', e)
    }
  }

  // Copy guest clipboard to local host clipboard
  const handleCopyGuestClipboard = async (textToCopy: string) => {
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(textToCopy)
        setCopySuccess(true)
        setTimeout(() => setCopySuccess(false), 2000)
      }
    } catch (e) {
      console.warn('Failed to copy to clipboard:', e)
    }
  }

  // Simulate typing text keystroke by keystroke into VNC
  const handleTypeKeystrokes = async () => {
    if (!rfbRef.current || !clipboardText || isTyping) return
    const rfb = rfbRef.current
    setIsTyping(true)
    stopTypingRef.current = false

    const chars = Array.from(clipboardText)
    const total = chars.length
    setTypeProgress({ current: 0, total })

    // Use a slight delay between characters to prevent QEMU key-buffer drops
    const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

    try {
      for (let i = 0; i < chars.length; i++) {
        if (stopTypingRef.current) break
        const ch = chars[i]
        const sym = charToKeysym(ch)
        if (sym > 0) {
          // Send key down and up
          rfb.sendKey(sym, undefined, true)
          rfb.sendKey(sym, undefined, false)
        }
        setTypeProgress({ current: i + 1, total })
        // Small interval between keystrokes (12ms is sweet spot for both Linux and Windows)
        await delay(12)
      }
    } finally {
      setIsTyping(false)
      setTypeProgress(null)
      stopTypingRef.current = false
    }
  }

  const handleStopTyping = () => {
    stopTypingRef.current = true
    setIsTyping(false)
    setTypeProgress(null)
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
      rfb.showDotCursor = true // Render dot cursor locally so mouse moves never wait for roundtrips
      rfb.clipViewport = false
      rfb.background = '#050505'
      applyModeSettings(rfb, mode)

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
      // Listen for clipboard events coming from the guest system
      rfb.addEventListener('clipboard', (event: Event) => {
        const detail = (event as CustomEvent<{ text: string }>).detail
        if (detail && detail.text) {
          setGuestClipboardText(detail.text)
          // Also automatically sync to editor if empty
          setClipboardText((prev) => prev ? prev : detail.text)
        }
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
    <div ref={containerRef} className="flex h-full flex-col overflow-hidden rounded-lg border border-gray-200 bg-white dark:bg-gray-900">
      <div className="flex shrink-0 items-center justify-between border-b border-gray-200 bg-gray-50 px-4 py-2.5 dark:border-gray-700 dark:bg-gray-800">
        <div className="flex items-center gap-2">
          <Monitor className="h-4 w-4 text-gray-600 dark:text-gray-300" />
          <span className="text-sm font-medium text-black dark:text-white">WebVNC - {containerName}</span>
          {status === 'connected' && <span className="rounded bg-green-100 px-1.5 py-0.5 text-xs text-green-700 dark:bg-green-950/60 dark:text-green-300">已连接</span>}
          {status === 'connecting' && <span className="rounded bg-yellow-100 px-1.5 py-0.5 text-xs text-yellow-700 dark:bg-yellow-950/60 dark:text-yellow-300">连接中...</span>}
          {status === 'disconnected' && <span className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300">已断开</span>}
          {status === 'error' && <span className="rounded bg-red-100 px-1.5 py-0.5 text-xs text-red-700 dark:bg-red-950/60 dark:text-red-300">连接失败</span>}
        </div>
        <div className="flex items-center gap-1.5">
          {/* Clipboard & Typing Tool Drawer Toggle */}
          <button
            onClick={() => {
              setShowClipboardDrawer(!showClipboardDrawer)
              if (!showClipboardDrawer && !clipboardText) {
                handleReadHostClipboard()
              }
            }}
            className={`inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium border transition-colors ${
              showClipboardDrawer
                ? 'bg-blue-600 text-white border-blue-600 shadow-sm'
                : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-100 dark:bg-gray-700 dark:text-gray-200 dark:border-gray-600'
            }`}
            title="打开剪贴板与模拟键入工具 (快速粘贴长命令、密码或同步虚拟机剪贴板)"
          >
            <Clipboard className="h-3.5 w-3.5" />
            <span>剪贴板</span>
          </button>

          {/* Smooth vs Clear mode toggle */}
          <button
            onClick={() => switchMode(mode === 'smooth' ? 'clear' : 'smooth')}
            className={`inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium border transition-colors ${
              mode === 'smooth'
                ? 'bg-blue-50 text-blue-700 border-blue-200 hover:bg-blue-100 dark:bg-blue-950/50 dark:text-blue-300 dark:border-blue-800'
                : 'bg-white text-gray-700 border-gray-200 hover:bg-gray-100 dark:bg-gray-700 dark:text-gray-200 dark:border-gray-600'
            }`}
            title={mode === 'smooth' ? '当前：流畅模式 (低带宽高帧率，本地光标)；点击切换清晰模式' : '当前：清晰模式 (高画质低压缩)；点击切换流畅模式'}
          >
            {mode === 'smooth' ? <Zap className="h-3 w-3 text-blue-600 dark:text-blue-400" /> : <Sparkles className="h-3 w-3 text-amber-500" />}
            <span>{mode === 'smooth' ? '流畅' : '清晰'}</span>
          </button>

          <button onClick={() => rfbRef.current?.sendCtrlAltDel()} className="inline-flex items-center gap-1 rounded px-2 py-1.5 text-xs text-gray-500 hover:bg-gray-200 dark:text-gray-400 dark:hover:bg-gray-700" title="发送 Ctrl+Alt+Del">
            <Send className="h-3.5 w-3.5" />
            Ctrl+Alt+Del
          </button>

          <button onClick={toggleFullscreen} className="rounded p-1.5 text-gray-500 hover:bg-gray-200 dark:text-gray-400 dark:hover:bg-gray-700" title={isFullscreen ? '退出全屏' : '全屏'}>
            {isFullscreen ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
          </button>

          <button onClick={connect} className="rounded p-1.5 text-xs text-gray-500 hover:bg-gray-200 dark:text-gray-400 dark:hover:bg-gray-700" title="重新连接">
            <RefreshCw className="h-3.5 w-3.5" />
          </button>

          <button onClick={onClose} className="rounded p-1.5 text-gray-500 hover:bg-gray-200 dark:text-gray-400 dark:hover:bg-gray-700" title="关闭">
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1 overflow-hidden bg-black">
        <div ref={screenRef} className="h-full w-full [&>div]:h-full [&>div]:w-full [&_canvas]:block" />

        {/* Floating Clipboard & Typing Tool Drawer */}
        {showClipboardDrawer && (
          <div className="absolute top-3 right-3 z-30 w-80 sm:w-96 rounded-xl border border-gray-200 bg-white/95 p-4 shadow-2xl backdrop-blur-md dark:border-gray-700 dark:bg-gray-900/95 transition-all">
            <div className="flex items-center justify-between mb-2.5 pb-2 border-b border-gray-100 dark:border-gray-800">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-gray-900 dark:text-gray-100">
                <Clipboard className="h-4 w-4 text-blue-600" />
                <span>剪贴板与模拟打字工具</span>
              </div>
              <button
                onClick={() => setShowClipboardDrawer(false)}
                className="rounded p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>

            <div className="space-y-3">
              {/* Text Area */}
              <div>
                <div className="flex items-center justify-between text-[11px] text-gray-500 mb-1">
                  <span>粘贴或输入要发送的内容：</span>
                  <div className="flex items-center gap-1.5">
                    <button
                      onClick={handleReadHostClipboard}
                      className="text-blue-600 hover:underline inline-flex items-center gap-0.5"
                      title="从本机剪贴板自动粘贴到此"
                    >
                      <Download className="h-3 w-3" /> 从本机粘贴
                    </button>
                    {clipboardText && (
                      <button
                        onClick={() => setClipboardText('')}
                        className="text-gray-400 hover:text-red-500 inline-flex items-center gap-0.5"
                        title="清空内容"
                      >
                        <Trash2 className="h-3 w-3" /> 清空
                      </button>
                    )}
                  </div>
                </div>
                <textarea
                  value={clipboardText}
                  onChange={(e) => setClipboardText(e.target.value)}
                  placeholder="在此输入或粘贴密码、长命令、Shell 脚本或 URL..."
                  rows={3}
                  className="w-full rounded-lg border border-gray-300 bg-gray-50 p-2 text-xs font-mono text-gray-900 focus:border-blue-500 focus:bg-white focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
                />
              </div>

              {/* Action Buttons */}
              <div className="grid grid-cols-2 gap-2">
                {/* 1. Simulate typing (Type text) */}
                <button
                  onClick={isTyping ? handleStopTyping : handleTypeKeystrokes}
                  disabled={!clipboardText}
                  className={`flex items-center justify-center gap-1.5 rounded-lg px-3 py-2 text-xs font-medium transition-all shadow-sm ${
                    isTyping
                      ? 'bg-amber-500 hover:bg-amber-600 text-white animate-pulse'
                      : 'bg-blue-600 hover:bg-blue-700 text-white disabled:opacity-50'
                  }`}
                  title="逐字模拟键盘敲击输入到当前光标处（100% 免驱通用，适合输入密码、无网络终端指令）"
                >
                  {isTyping ? (
                    <>
                      <Square className="h-3.5 w-3.5 fill-current" />
                      <span>停止键入 ({typeProgress?.current}/{typeProgress?.total})</span>
                    </>
                  ) : (
                    <>
                      <Keyboard className="h-3.5 w-3.5" />
                      <span>模拟键盘键入</span>
                    </>
                  )}
                </button>

                {/* 2. Send via RFB Clipboard */}
                <button
                  onClick={handleSendVNCClipboard}
                  disabled={!clipboardText}
                  className="flex items-center justify-center gap-1.5 rounded-lg border border-gray-300 bg-white hover:bg-gray-50 px-3 py-2 text-xs font-medium text-gray-800 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700 disabled:opacity-50"
                  title="发送到虚拟机的剪贴板（需要系统内支持 VNC 剪贴板，发送后在虚拟机内按 Ctrl+V）"
                >
                  {pasteSuccess ? (
                    <>
                      <Check className="h-3.5 w-3.5 text-emerald-600" />
                      <span className="text-emerald-600">已发送剪贴板</span>
                    </>
                  ) : (
                    <>
                      <Upload className="h-3.5 w-3.5 text-gray-500" />
                      <span>发送到剪贴板</span>
                    </>
                  )}
                </button>
              </div>

              {/* Guest clipboard display if available */}
              {guestClipboardText && (
                <div className="rounded-lg border border-emerald-100 bg-emerald-50/70 p-2.5 text-xs dark:border-emerald-900/40 dark:bg-emerald-950/40">
                  <div className="flex items-center justify-between text-[11px] font-medium text-emerald-800 dark:text-emerald-300 mb-1">
                    <span>虚拟机内剪贴板：</span>
                    <button
                      onClick={() => handleCopyGuestClipboard(guestClipboardText)}
                      className="inline-flex items-center gap-0.5 text-emerald-700 hover:underline dark:text-emerald-400"
                      title="复制到本机剪贴板"
                    >
                      {copySuccess ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                      <span>{copySuccess ? '已复制' : '复制到本机'}</span>
                    </button>
                  </div>
                  <p className="font-mono text-[11px] text-emerald-950 dark:text-emerald-100 truncate bg-white/60 dark:bg-black/40 px-1.5 py-1 rounded">
                    {guestClipboardText}
                  </p>
                </div>
              )}

              <div className="text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed border-t border-gray-100 dark:border-gray-800 pt-2">
                💡 <strong>提示</strong>：Windows 登录密码或 Linux 纯终端请点击 <strong>「模拟键盘键入」</strong>，无需任何驱动即可将文本自动打字输入光标处。
              </div>
            </div>
          </div>
        )}

        {(status === 'connecting' || status === 'error' || (status === 'disconnected' && errorMsg)) && (
          <div className={`absolute inset-x-0 bottom-0 border-t px-4 py-2 text-sm ${status === 'error' ? 'border-red-900 bg-red-950 text-red-100' : 'border-gray-800 bg-gray-950 text-gray-200'}`}>
            {status === 'connecting' ? '正在连接 KVM VNC 控制台...' : (errorMsg || 'WebVNC 已断开')}
          </div>
        )}
      </div>
    </div>
  )
}


