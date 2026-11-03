type TextMeasure = (text: string) => number

export type ChartAxisLabel = {
  tick: number
  lines: string[]
}

export type ChartLayout = {
  width: number
  height: number
  plot: { left: number; top: number; width: number; height: number }
  yLabels: ChartAxisLabel[]
  xLabels: ChartAxisLabel[]
  unitLines: string[]
  lineHeight: number
  fontTop: number
  fontHeight: number
}

// Use measured glyph widths, including fallback fonts for translated units.
// Unbroken numbers may wrap too: retaining their digits is preferable to clipping.
function wrapLabel(text: string, maxWidth: number, measure: TextMeasure): string[] {
  return text.split('\n').flatMap((paragraph) => {
    let remaining = Array.from(paragraph.trim())
    const lines: string[] = []
    while (remaining.length > 0) {
      if (measure(remaining.join('')) <= maxWidth) {
        lines.push(remaining.join(''))
        break
      }
      let count = 1
      while (count < remaining.length && measure(remaining.slice(0, count + 1).join('')) <= maxWidth) count++
      let lastSpace = -1
      for (let index = 0; index < count; index++) if (/\s/.test(remaining[index])) lastSpace = index
      if (lastSpace > 0) count = lastSpace
      lines.push(remaining.slice(0, count).join('').trimEnd())
      remaining = remaining.slice(count)
      while (remaining.length > 0 && /\s/.test(remaining[0])) remaining.shift()
    }
    return lines.length > 0 ? lines : ['']
  })
}

export function calculateChartLayout({
  width,
  yLabels,
  xLabels,
  unitLabel,
  measure,
  fontTop,
  fontHeight,
}: {
  width: number
  yLabels: string[]
  xLabels: string[]
  unitLabel?: string
  measure: TextMeasure
  fontTop: number
  fontHeight: number
}): ChartLayout {
  const padding = 4
  const right = 6
  const tickGap = 8
  const lineHeight = Math.ceil(Math.max(12, fontHeight + 2))
  // Long labels share the available width with a usable plot instead of imposing
  // a fixed left margin. On narrow cards their full value wraps within the gutter.
  const labelBudget = Math.max(12, Math.min(width * 0.42, width - padding - right - tickGap - 72))
  const wrappedY = yLabels.map((label, index) => ({ tick: 1 - index / Math.max(1, yLabels.length - 1), lines: wrapLabel(label, labelBudget, measure) }))
  const yWidth = Math.max(0, ...wrappedY.flatMap((label) => label.lines.map(measure)))
  const yHeight = Math.max(...wrappedY.map((label) => fontHeight + (label.lines.length - 1) * lineHeight))
  const left = padding + yWidth + tickGap
  const plotWidth = Math.max(1, width - left - right)
  const unitLines = unitLabel ? wrapLabel(unitLabel, Math.max(12, width - padding * 2), measure) : []
  const unitHeight = unitLines.length > 0 ? fontHeight + (unitLines.length - 1) * lineHeight + 6 : 0
  const top = padding + unitHeight + yHeight / 2
  const plotHeight = Math.max(104, (yHeight + 8) * 2)

  const widths = xLabels.map(measure)
  const showMiddle = widths[0] + widths[1] / 2 + tickGap <= plotWidth / 2
    && widths[2] + widths[1] / 2 + tickGap <= plotWidth / 2
  const xBudget = showMiddle ? plotWidth / 3 : Math.max(12, (plotWidth - tickGap) / 2)
  const wrappedX = (showMiddle ? [0, 1, 2] : [0, 2]).map((index) => ({
    tick: index / 2,
    lines: wrapLabel(xLabels[index], xBudget, measure),
  }))
  const xHeight = Math.max(...wrappedX.map((label) => fontHeight + (label.lines.length - 1) * lineHeight))

  return {
    width,
    height: Math.ceil(top + plotHeight + Math.max(yHeight / 2, tickGap + xHeight) + padding),
    plot: { left, top, width: plotWidth, height: plotHeight },
    yLabels: wrappedY,
    xLabels: wrappedX,
    unitLines,
    lineHeight,
    fontTop,
    fontHeight,
  }
}
