import { useEffect } from 'react'
import { useLocation } from 'react-router'
import { useLanguage } from '../contexts/LanguageContext'
import { shouldTranslateText, translateText } from '../utils/i18n'

const translatedTitleAttr = 'data-i18n-title-original'
const translatedPlaceholderAttr = 'data-i18n-placeholder-original'
const translatedAriaLabelAttr = 'data-i18n-aria-label-original'

const attributeNames = ['title', 'placeholder', 'aria-label'] as const
const translatedTextNodes = new Set<Text>()
const textOriginals = new WeakMap<Text, string>()
const wholeTextSelector = 'button,a,span,label,option,th,td,p,h1,h2,h3,h4,small'

export default function AutoTranslate() {
  const { language } = useLanguage()
  const location = useLocation()

  useEffect(() => {
    if (language === 'zh') {
      restoreTranslatedNodes(document.body)
      return
    }

    translateNode(document.body)

    const pending = new Set<Node>()
    let scheduled = false
    const flush = () => {
      scheduled = false
      const nodes = Array.from(pending)
      pending.clear()
      for (const node of nodes) {
        if (node.isConnected) translateNode(node)
      }
    }
    const schedule = (node: Node) => {
      pending.add(node)
      if (scheduled) return
      scheduled = true
      window.requestAnimationFrame(flush)
    }

    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        if (mutation.type === 'childList') {
          mutation.addedNodes.forEach(schedule)
        } else {
          schedule(mutation.target)
        }
      }
    })
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
      attributes: true,
      attributeFilter: [...attributeNames],
    })
    return () => observer.disconnect()
  }, [language, location.pathname, location.search])

  return null
}

function translateNode(root: Node) {
  if (root.nodeType === Node.TEXT_NODE) {
    translateTextNode(root as Text)
    return
  }
  if (!(root instanceof Element)) return
  if (shouldSkipElement(root)) return

  translateWholeTextElement(root)
  root.querySelectorAll<HTMLElement>(wholeTextSelector).forEach(translateWholeTextElement)
  translateElementAttributes(root)
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (!node.textContent || !shouldTranslateText(node.textContent)) return NodeFilter.FILTER_REJECT
      const parent = node.parentElement
      if (!parent || shouldSkipElement(parent)) {
        return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    },
  })

  const nodes: Text[] = []
  while (walker.nextNode()) nodes.push(walker.currentNode as Text)
  for (const node of nodes) translateTextNode(node)
  root.querySelectorAll<HTMLElement>('[title], [placeholder], [aria-label]').forEach(translateElementAttributes)
}

function translateTextNode(node: Text) {
  const original = node.textContent || ''
  if (!shouldTranslateText(original)) return
  const parent = node.parentElement
  if (!parent || shouldSkipElement(parent)) return
  const translated = translateText(original)
  if (translated === original) return
  textOriginals.set(node, original)
  translatedTextNodes.add(node)
  node.textContent = translated
}

function translateWholeTextElement(el: Element) {
  if (!(el instanceof HTMLElement) || shouldSkipElement(el) || !isSimpleTextElement(el)) return
  const original = el.textContent || ''
  if (!shouldTranslateText(original)) return
  const translated = translateText(original)
  if (translated === original) return

  const textNodes = directTextNodes(el)
  if (textNodes.length === 0) return
  textNodes.forEach((node, index) => {
    textOriginals.set(node, node.textContent || '')
    translatedTextNodes.add(node)
    node.textContent = index === 0 ? translated : ''
  })
}

function directTextNodes(el: HTMLElement) {
  return Array.from(el.childNodes).filter((node): node is Text => node.nodeType === Node.TEXT_NODE)
}

function translateElementAttributes(el: Element) {
  if (!(el instanceof HTMLElement)) return
  translateAttribute(el, 'title', translatedTitleAttr)
  translateAttribute(el, 'placeholder', translatedPlaceholderAttr)
  translateAttribute(el, 'aria-label', translatedAriaLabelAttr)
}

function restoreTranslatedNodes(root: ParentNode) {
  for (const node of Array.from(translatedTextNodes)) {
    if (!node.isConnected) {
      translatedTextNodes.delete(node)
      continue
    }
    if (root instanceof Document || root.contains(node)) {
      node.textContent = textOriginals.get(node) || node.textContent
      translatedTextNodes.delete(node)
    }
  }
  root.querySelectorAll<HTMLElement>(`[${translatedTitleAttr}]`).forEach((el) => {
    el.setAttribute('title', el.getAttribute(translatedTitleAttr) || '')
    el.removeAttribute(translatedTitleAttr)
  })
  root.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>(`[${translatedPlaceholderAttr}]`).forEach((el) => {
    el.setAttribute('placeholder', el.getAttribute(translatedPlaceholderAttr) || '')
    el.removeAttribute(translatedPlaceholderAttr)
  })
  root.querySelectorAll<HTMLElement>(`[${translatedAriaLabelAttr}]`).forEach((el) => {
    el.setAttribute('aria-label', el.getAttribute(translatedAriaLabelAttr) || '')
    el.removeAttribute(translatedAriaLabelAttr)
  })
}

function translateAttribute(el: HTMLElement, attr: 'title' | 'placeholder' | 'aria-label', originalAttr: string) {
  const storedOriginal = el.getAttribute(originalAttr)
  const original = storedOriginal || el.getAttribute(attr) || ''
  if (!shouldTranslateText(original)) return
  const translated = translateText(original)
  if (translated === original) return
  if (!storedOriginal) {
    el.setAttribute(originalAttr, original)
  }
  if (el.getAttribute(attr) !== translated) {
    el.setAttribute(attr, translated)
  }
}

function shouldSkipElement(el: Element) {
  return !!el.closest('script, style, code, pre, textarea, [data-no-translate]')
}

function isSimpleTextElement(el: HTMLElement) {
  if (!el.matches(wholeTextSelector)) return false
  if (el.querySelector('input, textarea, select, button, table, pre, code, canvas, iframe')) return false
  const textNodes = directTextNodes(el)
  if (textNodes.length === 0) return false
  return Array.from(el.children).every((child) => child.tagName.toLowerCase() === 'svg')
}
