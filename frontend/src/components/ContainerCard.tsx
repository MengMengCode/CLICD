import { useNavigate } from 'react-router'
import {
  Server,
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
          if (window.confirm(`确定要删除容器 ${container.name} 吗？此操作不可撤销。`)) {
            await deleteContainer(containerIdentifier)
          } else {
            return
          }
          break
      }
      onRefresh()
    } catch (err) {
      console.error('Action failed:', err)
      alert('操作失败')
    }
  }

  const statusColor = container.status === 'running' ? 'bg-green-500' : 'bg-red-500'
  const statusText = container.status === 'running' ? '运行中' : '已停止'

  return (
    <div className="bg-white border border-gray-200 rounded-lg p-5 hover:shadow-md transition-shadow">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 flex items-center justify-center">
            <Server className="w-5 h-5 text-gray-700" />
          </div>
          <div>
            <button
              onClick={() => navigate(`/container/${encodeURIComponent(String(containerIdentifier))}`)}
              className="font-semibold text-black hover:underline text-left"
            >
              {container.name}
            </button>
            <div className="flex items-center gap-1.5 mt-0.5">
              <span className={`w-1.5 h-1.5 rounded-full ${statusColor}`}></span>
              <span className="text-xs text-gray-500">{statusText}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Specs */}
      <div className="grid grid-cols-2 gap-3 mb-4">
        <div className="flex items-center gap-2 text-sm text-gray-600">
          <Cpu className="w-3.5 h-3.5" />
          <span>{container.vcpu} vCPU</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-gray-600">
          <MemoryStick className="w-3.5 h-3.5" />
          <span>{container.ram_mb} MB</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-gray-600">
          <HardDrive className="w-3.5 h-3.5" />
          <span>{container.disk_gb} GB</span>
        </div>
        <div className="flex items-center gap-2 text-sm text-gray-600">
          <Globe className="w-3.5 h-3.5" />
          <span>{formatNetworkLimit(container)}</span>
        </div>
      </div>

      {container.ip && (
        <div className="text-xs text-gray-400 mb-3">
          IP: {container.ip}
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-1.5 pt-3 border-t border-gray-100">
        {container.status !== 'running' ? (
          <button
            onClick={() => handleAction('start')}
            className="flex items-center gap-1 px-3 py-1.5 bg-green-600 text-white rounded text-xs hover:bg-green-700 transition-colors"
          >
            <Play className="w-3 h-3" />
            开机
          </button>
        ) : (
          <>
            <button
              onClick={() => handleAction('stop')}
              className="flex items-center gap-1 px-3 py-1.5 bg-yellow-500 text-white rounded text-xs hover:bg-yellow-600 transition-colors"
            >
              <Square className="w-3 h-3" />
              关机
            </button>
            <button
              onClick={() => handleAction('restart')}
              className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 text-white rounded text-xs hover:bg-blue-700 transition-colors"
            >
              <RotateCcw className="w-3 h-3" />
              重启
            </button>
          </>
        )}
        <div className="flex-1" />
        <button
          onClick={() => handleAction('delete')}
          className="flex items-center gap-1 px-3 py-1.5 text-red-600 hover:bg-red-50 rounded text-xs transition-colors"
        >
          <Trash2 className="w-3 h-3" />
          删除
        </button>
      </div>
    </div>
  )
}

function formatNetworkLimit(container: { network_bw_mbps?: number; network_down_mbps?: number; network_up_mbps?: number }) {
  const down = Math.max(0, Number(container.network_down_mbps || container.network_bw_mbps || 0))
  const up = Math.max(0, Number(container.network_up_mbps || container.network_bw_mbps || 0))
  if (down === 0 && up === 0) return '不限速'
  return `下 ${down || '不限'} / 上 ${up || '不限'} Mbps`
}
