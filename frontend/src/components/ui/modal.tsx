import { type ReactNode, useId, useRef } from 'react'
import { cn } from '@/lib/utils'
import { useLanguage } from '@/contexts/LanguageContext'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './dialog'

type ModalSize = 'sm' | 'default' | 'lg' | 'xl' | '2xl' | '3xl' | '4xl' | '5xl' | '6xl' | '7xl' | 'full'

export interface ModalProps {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  onClose?: () => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  size?: ModalSize
  maxWidth?: string
  className?: string
  bodyClassName?: string
  wide?: boolean
  flush?: boolean
  extra?: ReactNode
  footer?: ReactNode
  hideClose?: boolean
  fullScreen?: boolean
  dismissible?: boolean
}

const widths: Record<ModalSize, string> = {
  sm: 'max-w-sm', default: 'max-w-lg', lg: 'max-w-lg', xl: 'max-w-xl',
  '2xl': 'max-w-2xl', '3xl': 'max-w-3xl', '4xl': 'max-w-4xl',
  '5xl': 'max-w-5xl', '6xl': 'max-w-6xl', '7xl': 'max-w-7xl', full: 'max-w-[calc(100vw-2rem)]',
}

// Keep each screen's composition and form handlers while using the shared
// shadcn dialog for keyboard navigation, focus containment and scroll locking.
export function Modal({
  open = true, onOpenChange, onClose, title, description, children, size = 'lg',
  maxWidth, className, bodyClassName, wide, flush, extra, footer, hideClose,
  fullScreen, dismissible = true,
}: ModalProps) {
  const descriptionID = useId()
  const { t } = useLanguage()
  const returnFocus = useRef<HTMLElement | null>(null)
  const wasOpen = useRef(false)
  if (open && !wasOpen.current && typeof document !== 'undefined') {
    returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
  }
  wasOpen.current = open
  const changeOpen = (next: boolean) => {
    if (!next && !dismissible) return
    onOpenChange?.(next)
    if (!next) onClose?.()
  }
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent
        showCloseButton={!hideClose && dismissible}
        closeLabel={t('关闭')}
        aria-describedby={description ? descriptionID : undefined}
        onEscapeKeyDown={event => { if (!dismissible) event.preventDefault() }}
        onInteractOutside={event => { if (!dismissible) event.preventDefault() }}
        onCloseAutoFocus={event => {
          event.preventDefault()
          if (returnFocus.current?.isConnected) returnFocus.current.focus({ preventScroll: true })
        }}
        className={cn('flex max-h-[90vh] flex-col gap-0 overflow-hidden p-0', maxWidth || widths[wide ? '4xl' : size],
          fullScreen && 'inset-3 left-3 top-3 h-[calc(100dvh-1.5rem)] max-h-none w-[calc(100vw-1.5rem)] max-w-none translate-x-0 translate-y-0', className)}
      >
        <DialogHeader className="shrink-0 border-b px-5 py-4 text-left">
          <div className="flex min-w-0 items-center justify-between gap-3 pr-7">
            <DialogTitle className="min-w-0 text-base leading-6">{title}</DialogTitle>
            {extra && <div className="flex shrink-0 items-center gap-2">{extra}</div>}
          </div>
          {description && <DialogDescription id={descriptionID}>{description}</DialogDescription>}
        </DialogHeader>
        <div className={cn('min-h-0 flex-1 overflow-y-auto', flush ? 'p-0' : 'p-5', bodyClassName)}>{children}</div>
        {footer && <DialogFooter className="shrink-0 border-t px-5 py-4">{footer}</DialogFooter>}
      </DialogContent>
    </Dialog>
  )
}
