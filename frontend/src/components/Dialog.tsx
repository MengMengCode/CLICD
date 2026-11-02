import { useState, useCallback, createContext, useContext, ReactNode, useEffect, useRef } from 'react'
import { AlertTriangle } from 'lucide-react'
import { toast } from 'sonner'
import { useLanguage } from '../contexts/LanguageContext'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from './ui/alert-dialog'
import { Toaster } from './ui/sonner'

interface DialogState { open: boolean; title: string; message: string }
interface DialogContextType {
  confirm: (title: string, message: string) => Promise<boolean>
  alert: (title: string, message: string) => Promise<void>
}
const DialogContext = createContext<DialogContextType | undefined>(undefined)

export function DialogProvider({ children }: { children: ReactNode }) {
  const [dialog, setDialog] = useState<DialogState>({ open: false, title: '', message: '' })
  const resolver = useRef<((value: boolean) => void) | undefined>(undefined)
  const returnFocus = useRef<HTMLElement | null>(null)
  const { t } = useLanguage()
  const confirm = useCallback((title: string, message: string) => new Promise<boolean>(resolve => {
    resolver.current?.(false)
    returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    resolver.current = resolve
    setDialog({ open: true, title, message })
  }), [])
  const alert = useCallback((title: string, message: string) => {
    const options = { description: t(message), duration: 4200 }
    const heading = t(title)
    if (/失败|错误|异常|不可用|failed|error/i.test(title)) toast.error(heading, options)
    else if (/提示|警告|未配置|格式|配额|warning/i.test(title)) toast.warning(heading, options)
    else if (/完成|成功|已保存|success/i.test(title)) toast.success(heading, options)
    else toast.info(heading, options)
    return Promise.resolve()
  }, [t])
  const close = (value: boolean) => {
    const resolve = resolver.current
    resolver.current = undefined
    setDialog({ open: false, title: '', message: '' })
    resolve?.(value)
  }
  useEffect(() => () => { resolver.current?.(false) }, [])

  return (
    <DialogContext.Provider value={{ confirm, alert }}>
      {children}
      <Toaster position="top-right" closeButton visibleToasts={4} />
      <AlertDialog open={dialog.open} onOpenChange={open => { if (!open) close(false) }}>
        <AlertDialogContent className="z-[100] max-w-sm" onCloseAutoFocus={event => {
          event.preventDefault()
          if (returnFocus.current?.isConnected) returnFocus.current.focus({ preventScroll: true })
        }}>
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-3 text-base"><AlertTriangle className="h-4 w-4 shrink-0" />{t(dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription className="whitespace-pre-wrap break-words">{t(dialog.message)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => close(false)}>{t('取消')}</AlertDialogCancel>
            <AlertDialogAction onClick={() => close(true)}>{t('确认')}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DialogContext.Provider>
  )
}
export function useDialog() {
  const ctx = useContext(DialogContext)
  if (!ctx) throw new Error('useDialog must be used within DialogProvider')
  return ctx
}
