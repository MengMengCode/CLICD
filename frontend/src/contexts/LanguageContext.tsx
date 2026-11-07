import { ReactNode, createContext, useContext, useEffect, useMemo, useState } from 'react'
import { translateText } from '../utils/i18n'
import { getLanguage, updateLanguage } from '../services/api'
import { useAuth } from './AuthContext'

export type Language = 'zh' | 'en'

interface LanguageContextValue {
  language: Language
  setLanguage: (language: Language) => void
  toggleLanguage: () => Promise<void>
  t: (value: string) => string
}

const LanguageContext = createContext<LanguageContextValue | undefined>(undefined)
const languagePreferenceKey = 'clicd_language'

function storedLanguage(): Language | undefined {
  const saved = localStorage.getItem(languagePreferenceKey)
  return saved === 'zh' || saved === 'en' ? saved : undefined
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const { isAuthenticated, isSubUser } = useAuth()
  const [language, setLanguageState] = useState<Language>(() => storedLanguage() || 'zh')

  const value = useMemo<LanguageContextValue>(() => {
    const changeLanguage = async (next: Language) => {
      setLanguageState(next)
      localStorage.setItem(languagePreferenceKey, next)
      if (isAuthenticated && !isSubUser) {
        await updateLanguage(next).catch(() => {})
      }
    }
    return {
      language,
      setLanguage: next => { void changeLanguage(next) },
      toggleLanguage: () => changeLanguage(language === 'zh' ? 'en' : 'zh'),
      t: (text: string) => language === 'en' ? translateText(text) : text,
    }
  }, [isAuthenticated, isSubUser, language])

  useEffect(() => {
    getLanguage()
      .then((res) => {
        const serverLanguage = res.data.data?.language
        if (!storedLanguage() && (serverLanguage === 'zh' || serverLanguage === 'en')) {
          setLanguageState(serverLanguage)
        }
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    document.documentElement.lang = language === 'en' ? 'en' : 'zh-CN'
    document.documentElement.dataset.language = language
  }, [language])

  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>
}

export function useLanguage() {
  const context = useContext(LanguageContext)
  if (!context) {
    throw new Error('useLanguage must be used within LanguageProvider')
  }
  return context
}
