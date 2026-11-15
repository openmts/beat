import { useEffect, useId, useMemo, useRef, useState } from "react";
import { formatClock, formatPercent } from "@/lib/format";
import { cn } from "@/lib/utils";

export interface TrendSeries {
  key: string;
  label: string;
  color: string;
  values: Array<number | null>;
}

export function TrendChart({
  timestamps,
  series,
  height = 250,
  yMax = 100,
  yTicks,
  className,
  valueFormatter = formatPercent,
  yTickFormatter,
  ariaLabel = "指标趋势图",
}: {
  timestamps: string[];
  series: TrendSeries[];
  height?: number;
  yMax?: number;
  yTicks?: number[];
  className?: string;
  valueFormatter?: (value: number) => string;
  yTickFormatter?: (value: number) => string;
  ariaLabel?: string;
}) {
  const gradientID = useId().replace(/:/g, "");
  const [hovered, setHovered] = useState<number | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const lastPointerTypeRef = useRef<string>("mouse");
  const width = 900;
  const plot = { left: 66, right: 18, top: 16, bottom: 30 };
  const innerWidth = width - plot.left - plot.right;
  const innerHeight = height - plot.top - plot.bottom;
  const count = timestamps.length;
  const safeYMax = yMax > 0 ? yMax : 1;
  const xAt = (index: number) => plot.left + (count <= 1 ? innerWidth / 2 : index / (count - 1) * innerWidth);
  const yAt = (value: number) => plot.top + innerHeight - Math.max(0, Math.min(safeYMax, value)) / safeYMax * innerHeight;
  const paths = useMemo(() => series.map((item) => {
    const segments: Array<{ line: string; area: string }> = [];
    let current: Array<{ x: number; y: number }> = [];
    function flush() {
      if (current.length === 0) return;
      const points = current.map((point) => `${point.x.toFixed(2)},${point.y.toFixed(2)}`);
      const area = current.length > 1
        ? `M ${current[0].x} ${plot.top + innerHeight} L ${points.join(" L ")} L ${current[current.length - 1].x} ${plot.top + innerHeight} Z`
        : "";
      segments.push({ line: points.join(" "), area });
      current = [];
    }
    item.values.forEach((value, index) => {
      if (value === null || !Number.isFinite(value)) {
        flush();
        return;
      }
      current.push({ x: xAt(index), y: yAt(value) });
    });
    flush();
    return { ...item, segments };
  }), [series, timestamps.length, height, safeYMax]);
  const xLabels = count === 0 ? [] : Array.from({ length: 5 }, (_, index) => Math.round(index / 4 * (count - 1)));
  const yLabels = yTicks ?? [1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * safeYMax);
  const formatYTick = yTickFormatter ?? ((value: number) => safeYMax === 100 ? `${Math.round(value)}%` : formatPercent(value));

  useEffect(() => {
    function dismiss(event: PointerEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setHovered(null);
      }
    }
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, []);

  function move(event: React.PointerEvent<SVGSVGElement>) {
    lastPointerTypeRef.current = event.pointerType;
    const rect = event.currentTarget.getBoundingClientRect();
    const x = (event.clientX - rect.left) / rect.width * width;
    const ratio = Math.max(0, Math.min(1, (x - plot.left) / innerWidth));
    setHovered(count > 0 ? Math.round(ratio * (count - 1)) : null);
  }

  function leave(event: React.PointerEvent<SVGSVGElement>) {
    if (event.pointerType === "mouse" && lastPointerTypeRef.current === "mouse") setHovered(null);
  }

  const hoveredX = hovered === null ? 0 : xAt(hovered);
  const tooltipLeft = hoveredX / width * 100;

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      {timestamps.length === 0 ? (
        <div className="grid h-56 place-items-center text-xs text-muted-foreground">暂无时序数据</div>
      ) : (
        <>
          <svg viewBox={`0 0 ${width} ${height}`} className="w-full touch-pan-y" role="img" aria-label={ariaLabel} onPointerDown={move} onPointerMove={move} onPointerLeave={leave}>
            <defs>
              {paths.map((item) => (
                <linearGradient key={item.key} id={`${gradientID}-${item.key}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={item.color} stopOpacity="0.18" />
                  <stop offset="100%" stopColor={item.color} stopOpacity="0" />
                </linearGradient>
              ))}
            </defs>
            {yLabels.map((label) => {
              const y = yAt(label);
              return (
                <g key={label}>
                  <line x1={plot.left} x2={width - plot.right} y1={y} y2={y} stroke="hsl(var(--border))" strokeDasharray="3 5" opacity="0.75" />
                  <text x={plot.left - 10} y={y + 4} textAnchor="end" fontSize="10" fill="hsl(var(--muted-foreground))">{formatYTick(label)}</text>
                </g>
              );
            })}
            {paths.map((item) => <g key={item.key}>{item.segments.map((segment, index) => <g key={index}>
              {segment.area ? <path d={segment.area} fill={`url(#${gradientID}-${item.key})`} /> : null}
              <polyline points={segment.line} fill="none" stroke={item.color} strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
            </g>)}</g>)}
            {xLabels.map((index, position) => (
              <text key={`${index}-${position}`} x={xAt(index)} y={height - 8} textAnchor={position === 0 ? "start" : position === 4 ? "end" : "middle"} fontSize="10" fill="hsl(var(--muted-foreground))">
                {timestamps[index] ? formatClock(timestamps[index]) : ""}
              </text>
            ))}
            {hovered !== null ? (
              <g>
                <line x1={hoveredX} x2={hoveredX} y1={plot.top} y2={plot.top + innerHeight} stroke="hsl(var(--muted-foreground))" strokeDasharray="3 4" opacity="0.6" />
                {paths.map((item) => item.values[hovered] == null ? null : (
                  <circle key={item.key} cx={hoveredX} cy={yAt(item.values[hovered])} r="3.5" fill="hsl(var(--card))" stroke={item.color} strokeWidth="2" />
                ))}
              </g>
            ) : null}
          </svg>
          {hovered !== null ? (
            <div className="pointer-events-none absolute top-2 z-10 w-40 rounded-lg border bg-popover/95 p-2.5 text-xs shadow-float backdrop-blur" style={{ left: `clamp(8px, calc(${tooltipLeft}% - 80px), calc(100% - 168px))` }}>
              <p className="mb-1.5 text-[10px] text-muted-foreground">{formatClock(timestamps[hovered])}</p>
              {series.map((item) => (
                <div key={item.key} className="flex items-center justify-between gap-3 py-0.5">
                  <span className="flex items-center gap-1.5 text-muted-foreground"><span className="h-1.5 w-1.5 rounded-full" style={{ background: item.color }} />{item.label}</span>
                  <span className="font-medium tabular-nums">{item.values[hovered] == null ? "—" : valueFormatter(item.values[hovered])}</span>
                </div>
              ))}
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}

