import { useState, useCallback, createContext, useContext, ReactNode, useEffect, useRef } from 'react'
import { AlertTriangle, CheckCircle2, CircleAlert, Info, X } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'

interface DialogState {
  open: boolean
  title: string
  message: string
  resolve?: (value: boolean) => void
}

type ToastTone = 'success' | 'error' | 'warning' | 'info'

interface ToastState {
  id: number
  title: string
  message: string
  tone: ToastTone
}

interface DialogContextType {
  confirm: (title: string, message: string) => Promise<boolean>
  alert: (title: string, message: string) => Promise<void>
}

const DialogContext = createContext<DialogContextType | undefined>(undefined)

const toastStyles = {
  success: { icon: CheckCircle2, iconClass: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-950 dark:text-emerald-300', borderClass: 'border-emerald-200 dark:border-emerald-800' },
  error: { icon: CircleAlert, iconClass: 'bg-red-50 text-red-600 dark:bg-red-950 dark:text-red-300', borderClass: 'border-red-200 dark:border-red-800' },
  warning: { icon: AlertTriangle, iconClass: 'bg-amber-50 text-amber-600 dark:bg-amber-950 dark:text-amber-300', borderClass: 'border-amber-200 dark:border-amber-800' },
  info: { icon: Info, iconClass: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300', borderClass: 'border-gray-200 dark:border-gray-700' },
}

function toastTone(title: string): ToastTone {
  if (/失败|错误|异常|不可用|failed|error/i.test(title)) return 'error'
  if (/提示|警告|未配置|格式|配额|warning/i.test(title)) return 'warning'
  if (/完成|成功|已保存|success/i.test(title)) return 'success'
  return 'info'
}

export function DialogProvider({ children }: { children: ReactNode }) {
  const [dialog, setDialog] = useState<DialogState>({ open: false, title: '', message: '' })
  const [toasts, setToasts] = useState<ToastState[]>([])
  const toastID = useRef(0)
  const toastTimers = useRef(new Map<number, number>())
  const { t } = useLanguage()

  const confirm = useCallback((title: string, message: string) => {
    return new Promise<boolean>((resolve) => {
      setDialog({ open: true, title, message, resolve })
    })
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
    const timer = toastTimers.current.get(id)
    if (timer !== undefined) window.clearTimeout(timer)
    toastTimers.current.delete(id)
  }, [])

  const alert = useCallback((title: string, message: string) => {
    const id = ++toastID.current
    setToasts((current) => [...current, { id, title, message, tone: toastTone(title) }].slice(-4))
    const timer = window.setTimeout(() => dismissToast(id), 4200)
    toastTimers.current.set(id, timer)
    return Promise.resolve()
  }, [dismissToast])

  useEffect(() => () => {
    toastTimers.current.forEach((timer) => window.clearTimeout(timer))
    toastTimers.current.clear()
  }, [])

  const close = (result: boolean) => {
    dialog.resolve?.(result)
    setDialog({ open: false, title: '', message: '' })
  }

  return (
    <DialogContext.Provider value={{ confirm, alert }}>
      {children}
      <div className="pointer-events-none fixed right-4 top-4 z-[120] flex w-[calc(100vw-2rem)] max-w-sm flex-col gap-2" aria-live="polite" aria-atomic="true">
        {toasts.map((toast) => {
          const style = toastStyles[toast.tone]
          const ToastIcon = style.icon
          return (
            <div key={toast.id} className={`pointer-events-auto rounded-lg border bg-white shadow-lg dark:bg-gray-900 dark:shadow-black/40 ${style.borderClass}`} role="status">
              <div className="flex items-start gap-3 p-3.5">
                <div className={`mt-0.5 flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full ${style.iconClass}`}>
                  <ToastIcon className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-semibold text-gray-900 dark:text-white">{t(toast.title)}</div>
                  <div className="mt-0.5 break-words text-sm leading-5 text-gray-600 dark:text-gray-300">{t(toast.message)}</div>
                </div>
                <button onClick={() => dismissToast(toast.id)} className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-black dark:text-gray-500 dark:hover:bg-gray-800 dark:hover:text-white" title={t('关闭')}>
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
          )
        })}
      </div>
      {dialog.open && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/50 p-4 dark:bg-black/70">
          <div className="w-full max-w-sm overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900">
            <div className="flex items-center gap-3 border-b border-gray-100 px-5 py-4 dark:border-gray-700">
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-amber-50 text-amber-600 dark:bg-amber-950 dark:text-amber-300">
                <AlertTriangle className="h-4 w-4" />
              </div>
              <h3 className="flex-1 text-sm font-semibold text-black dark:text-white">{t(dialog.title)}</h3>
            </div>
            <div className="px-5 py-4">
              <p className="text-sm text-gray-600 dark:text-gray-300">{t(dialog.message)}</p>
            </div>
            <div className="flex justify-end gap-2 border-t border-gray-100 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-800">
              <button
                onClick={() => close(false)}
                className="rounded-md px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                {t('取消')}
              </button>
              <button
                onClick={() => close(true)}
                className="rounded-md bg-black px-4 py-2 text-sm text-white transition-colors hover:bg-gray-800 dark:bg-white dark:text-black dark:hover:bg-gray-200"
              >
                {t('确认')}
              </button>
            </div>
          </div>
        </div>
      )}
    </DialogContext.Provider>
  )
}

export function useDialog() {
  const ctx = useContext(DialogContext)
  if (!ctx) throw new Error('useDialog must be used within DialogProvider')
  return ctx
}
