import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Label } from '../components/ui/label'
import { Input } from '../components/ui/input'
import { Alert, AlertDescription } from '../components/ui/alert'
import { FormEvent, useState } from 'react'
import { Lock, User } from 'lucide-react'
import AppIcon from '../components/AppIcon'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'
import AutoTranslate from '../components/AutoTranslate'
import BrowserDialogTranslator from '../components/BrowserDialogTranslator'

function LanguageIcon({ className = '' }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 1024 1024" version="1.1" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <path d="M213.333333 640v85.333333a85.333333 85.333333 0 0 0 78.933334 85.12L298.666667 810.666667h128v85.333333H298.666667a170.666667 170.666667 0 0 1-170.666667-170.666667v-85.333333h85.333333z m554.666667-213.333333l187.733333 469.333333h-91.946666l-51.242667-128h-174.506667l-51.157333 128h-91.904L682.666667 426.666667h85.333333z m-42.666667 123.093333L672.128 682.666667h106.325333L725.333333 549.76zM341.333333 85.333333v85.333334h170.666667v298.666666H341.333333v128H256v-128H85.333333V170.666667h170.666667V85.333333h85.333333z m384 42.666667a170.666667 170.666667 0 0 1 170.666667 170.666667v85.333333h-85.333333V298.666667a85.333333 85.333333 0 0 0-85.333334-85.333334h-128V128h128zM256 256H170.666667v128h85.333333V256z m170.666667 0H341.333333v128h85.333334V256z" fill="currentColor" />
    </svg>
  )
}

export default function Login() {
  const { login, accessCodeLogin } = useAuth()
  const { language, toggleLanguage, t } = useLanguage()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // Check for access code in URL
  const urlParams = new URLSearchParams(window.location.search)
  const accessCode = urlParams.get('code') || ''

  const isAccessCodeLogin = !!accessCode

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setLoading(true)

    try {
      if (isAccessCodeLogin) {
        await accessCodeLogin(accessCode, password)
      } else {
        await login(username, password)
      }
    } catch (err: unknown) {
      const error = err as { response?: { status?: number; data?: { message?: string } } }
      if (error.response?.status === 401) {
        setError(t(isAccessCodeLogin ? '访问码或密码错误' : '用户名或密码错误'))
      } else {
        setError(error.response?.data?.message || t('登录失败，请检查用户名和密码'))
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-muted px-4">
      <AutoTranslate />
      <BrowserDialogTranslator />
      <Button variant="outline"
        type="button"
        onClick={() => { void toggleLanguage() }}
        className="h-auto absolute right-4 top-4 inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium"
      >
        <LanguageIcon className="h-3.5 w-3.5" />
        {language === 'en' ? '中文' : 'English'}
      </Button>
      <div className="w-full max-w-md">
        <Card className="p-8">
            <div className="flex flex-col items-center mb-8">
              <div className="w-16 h-16 flex items-center justify-center mb-4">
                <AppIcon className="w-10 h-10" />
              </div>
              <h1 className="text-2xl font-bold text-foreground">CLICD</h1>
              <p className="text-muted-foreground mt-1 text-sm">{isAccessCodeLogin ? '容器管理登录' : 'Container Manager'}</p>
            </div>

            <form onSubmit={handleSubmit} className="space-y-5">
              {error && (
                <Alert variant="destructive" className="px-4 py-3">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}

              {!isAccessCodeLogin && (
                <div>
                  <Label htmlFor="login-username" className="block mb-1.5 leading-normal">
                    {t('用户名')}
                  </Label>
                  <div className="relative">
                    <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                      <User className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <Input
                      id="login-username"
                      type="text"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      className="h-auto block w-full pl-10 pr-3 py-2.5 text-sm"
                      placeholder={t('输入用户名')}
                      required
                      autoComplete="username"
                    />
                  </div>
                </div>
              )}

            <div>
              <Label htmlFor="login-password" className="block mb-1.5 leading-normal">
                {t('密码')}
              </Label>
              <div className="relative">
                <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                  <Lock className="h-4 w-4 text-muted-foreground" />
                </div>
                <Input
                  id="login-password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  className="h-auto block w-full pl-10 pr-3 py-2.5 text-sm"
                  placeholder={t('输入密码')}
                  required
                  autoComplete="current-password"
                />
              </div>
            </div>

            <Button variant="default"
              type="submit"
              disabled={loading}
              className="h-auto w-full py-2.5 text-sm font-medium"
            >
              {loading ? t('登录中...') : t('登录')}
            </Button>
          </form>
        </Card>

        <p className="text-center text-xs text-muted-foreground mt-6">CLICD v1.1.29</p>
      </div>
    </div>
  )
}
