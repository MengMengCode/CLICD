export function formatMetricRate(bytesPerSecond: number, fractionDigits = 2): string {
  if (!Number.isFinite(bytesPerSecond)) return '0 B/s'
  if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(0)} B/s`

  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytesPerSecond / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(fractionDigits)} ${units[unit]}/s`
}
