import { useDialog } from './Dialog'
import { Card } from './ui/card'
import { Button } from './ui/button'
import { WorkloadIdentity, WorkloadStatus } from './WorkloadStatus'
import { useNavigate } from 'react-router'
import {
  Cpu,
  HardDrive,
  MemoryStick,
  Globe,
  Play,
  Square,
  RotateCcw,
  Trash2,
} from 'lucide-react'
import { Container, startContainer, stopContainer, restartContainer, deleteContainer } from '../services/api'

interface ContainerCardProps {
  container: Container
  onRefresh: () => void
}

export default function ContainerCard({ container, onRefresh }: ContainerCardProps) {
  const navigate = useNavigate()
  const dialog = useDialog()
  const containerIdentifier = container.uuid || container.id

  const handleAction = async (action: string) => {
    try {
      switch (action) {
        case 'start':
          await startContainer(containerIdentifier)
          break
        case 'stop':
          await stopContainer(containerIdentifier)
          break
        case 'restart':
          await restartContainer(containerIdentifier)
          break
        case 'delete':
          if (await dialog.confirm('删除容器', `确定要删除容器 ${container.name} 吗？此操作不可撤销。`)) {
            await deleteContainer(containerIdentifier)
          } else {
            return
          }
          break
      }
      onRefresh()
    } catch (err) {
      console.error('Action failed:', err)
      await dialog.alert('操作失败', '请稍后重试')
    }
  }

  const statusState = container.status === 'running' ? 'running' : container.status === 'initializing' ? 'pending' : container.status === 'failed' || container.status === 'error' ? 'error' : 'stopped'
  const statusText = container.status === 'running' ? '运行中' : container.status === 'initializing' ? '正在初始化' : statusState === 'error' ? '运行异常' : '已停止'

  return (
    <Card className="p-5">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex min-w-0 items-center gap-3">
          <WorkloadIdentity runtime={container.virtualization} name={container.name} />
          <div className="min-w-0">
            <Button variant="link"
              onClick={() => navigate(`/container/${encodeURIComponent(String(containerIdentifier))}`)}
              className="h-auto max-w-full justify-start truncate p-0 text-left font-semibold"
              title={container.name}
            >
              <span className="truncate">{container.name}</span>
            </Button>
            <div className="mt-1">
              <WorkloadStatus state={statusState} label={statusText} loading={statusState === 'pending'} />
            </div>
          </div>
        </div>
      </div>

      {/* Specs */}
      <div className="grid grid-cols-2 gap-3 mb-4">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Cpu className="w-3.5 h-3.5" />
          <span>{container.vcpu} vCPU</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <MemoryStick className="w-3.5 h-3.5" />
          <span>{container.ram_mb} MB</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <HardDrive className="w-3.5 h-3.5" />
          <span>{container.disk_gb} GB</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Globe className="w-3.5 h-3.5" />
          <span>{formatNetworkLimit(container)}</span>
        </div>
      </div>

      {container.ip && (
        <div className="text-xs text-muted-foreground mb-3">
          IP: {container.ip}
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-1.5 pt-3 border-t border-border">
        {container.status !== 'running' ? (
          <Button variant="default"
            onClick={() => handleAction('start')}
            className="h-auto flex items-center gap-1 px-3 py-1.5 text-xs"
          >
            <Play className="w-3 h-3" />
            开机
          </Button>
        ) : (
          <>
            <Button variant="default"
              onClick={() => handleAction('stop')}
              className="h-auto flex items-center gap-1 px-3 py-1.5 text-xs"
            >
              <Square className="w-3 h-3" />
              关机
            </Button>
            <Button variant="default"
              onClick={() => handleAction('restart')}
              className="h-auto flex items-center gap-1 px-3 py-1.5 text-xs"
            >
              <RotateCcw className="w-3 h-3" />
              重启
            </Button>
          </>
        )}
        <div className="flex-1" />
        <Button variant="destructive"
          onClick={() => handleAction('delete')}
          className="h-auto flex items-center gap-1 px-3 py-1.5 text-xs"
        >
          <Trash2 className="w-3 h-3" />
          删除
        </Button>
      </div>
    </Card>
  )
}

function formatNetworkLimit(container: { network_bw_mbps?: number; network_down_mbps?: number; network_up_mbps?: number }) {
  const down = Math.max(0, Number(container.network_down_mbps || container.network_bw_mbps || 0))
  const up = Math.max(0, Number(container.network_up_mbps || container.network_bw_mbps || 0))
  if (down === 0 && up === 0) return '不限速'
  return `下 ${down || '不限'} / 上 ${up || '不限'} Mbps`
}
