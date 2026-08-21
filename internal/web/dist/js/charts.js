// Lightweight, dependency-free SVG chart rendering engine for Control Plane metrics.
import { esc } from './dom.js';

export function renderAreaChart({
  title = '',
  data = [],
  xLabel = item => item.label,
  yValue = item => item.value,
  color = '#38bdf8',
  gradientId = 'area-grad',
  unit = '',
  height = 160,
  emptyText = 'No data available'
}) {
  if (!data || data.length === 0) {
    return `<div class="chart-box">
      <div class="chart-header"><strong>${esc(title)}</strong></div>
      <div class="chart-empty">${esc(emptyText)}</div>
    </div>`;
  }

  const values = data.map(yValue);
  const maxVal = Math.max(...values, 1);
  const minVal = 0;
  const padding = { top: 20, right: 15, bottom: 28, left: 45 };
  const w = 500;
  const h = height;
  const chartW = w - padding.left - padding.right;
  const chartH = h - padding.top - padding.bottom;

  const points = data.map((item, i) => {
    const x = padding.left + (data.length > 1 ? (i / (data.length - 1)) * chartW : chartW / 2);
    const val = yValue(item);
    const y = padding.top + chartH - ((val - minVal) / (maxVal - minVal)) * chartH;
    return { x, y, val, label: xLabel(item) };
  });

  // Build SVG path
  let pathD = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length; i++) {
    const prev = points[i - 1];
    const curr = points[i];
    const cpX = (prev.x + curr.x) / 2;
    pathD += ` C ${cpX} ${prev.y}, ${cpX} ${curr.y}, ${curr.x} ${curr.y}`;
  }

  const areaD = `${pathD} L ${points[points.length - 1].x} ${padding.top + chartH} L ${points[0].x} ${padding.top + chartH} Z`;

  // Grid lines and Y axis
  const gridLines = [0, 0.5, 1].map(ratio => {
    const y = padding.top + chartH - ratio * chartH;
    const val = (minVal + ratio * (maxVal - minVal));
    const label = val >= 1000000 ? `${(val / 1000000).toFixed(1)}M` : val >= 1000 ? `${(val / 1000).toFixed(1)}k` : Math.round(val);
    return `<line x1="${padding.left}" y1="${y}" x2="${w - padding.right}" y2="${y}" stroke="var(--chart-grid)" stroke-dasharray="3,3" />
            <text x="${padding.left - 8}" y="${y + 4}" fill="var(--chart-label)" font-size="10" text-anchor="end" font-family="ui-monospace, monospace">${label}</text>`;
  }).join('');

  // X labels (first, middle, last)
  const xLabelsToShow = [];
  if (points.length > 0) xLabelsToShow.push(points[0]);
  if (points.length > 2) xLabelsToShow.push(points[Math.floor(points.length / 2)]);
  if (points.length > 1) xLabelsToShow.push(points[points.length - 1]);

  const xLabelsSvg = xLabelsToShow.map(p => {
    return `<text x="${p.x}" y="${h - 8}" fill="var(--chart-label)" font-size="10" text-anchor="middle" font-family="ui-monospace, monospace">${esc(p.label)}</text>`;
  }).join('');

  const circles = points.map(p => {
    return `<circle class="chart-point" cx="${p.x}" cy="${p.y}" r="3.5" fill="${color}" stroke="var(--chart-point-stroke)" stroke-width="2">
      <title>${esc(p.label)}: ${esc(p.val)} ${esc(unit)}</title>
    </circle>`;
  }).join('');

  return `<div class="chart-box">
    <div class="chart-header">
      <strong>${esc(title)}</strong>
      <span class="chart-max">${esc(Math.round(maxVal))} ${esc(unit)} max</span>
    </div>
    <div class="chart-svg-wrap">
      <svg viewBox="0 0 ${w} ${h}" class="chart-svg" preserveAspectRatio="none">
        <defs>
          <linearGradient id="${gradientId}" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="${color}" stop-opacity="0.32" />
            <stop offset="100%" stop-color="${color}" stop-opacity="0.0" />
          </linearGradient>
        </defs>
        ${gridLines}
        <path d="${areaD}" fill="url(#${gradientId})" />
        <path d="${pathD}" fill="none" stroke="${color}" stroke-width="2.2" stroke-linecap="round" />
        ${circles}
        ${xLabelsSvg}
      </svg>
    </div>
  </div>`;
}

export function renderDonutChart({
  title = '',
  slices = [], // [{ label, value, color }]
  size = 140
}) {
  const total = slices.reduce((acc, s) => acc + (s.value || 0), 0);
  if (total === 0) {
    return `<div class="chart-box donut-box">
      <div class="chart-header"><strong>${esc(title)}</strong></div>
      <div class="chart-empty">No traffic yet</div>
    </div>`;
  }

  const cx = size / 2;
  const cy = size / 2;
  const r = size * 0.38;
  const strokeWidth = size * 0.18;
  const circumference = 2 * Math.PI * r;

  let accumulatedPercent = 0;
  const paths = slices.map(slice => {
    if (!slice.value) return '';
    const percent = slice.value / total;
    const strokeDasharray = `${percent * circumference} ${circumference}`;
    const strokeDashoffset = -accumulatedPercent * circumference;
    accumulatedPercent += percent;

    return `<circle cx="${cx}" cy="${cy}" r="${r}" fill="none" stroke="${slice.color}" stroke-width="${strokeWidth}"
      stroke-dasharray="${strokeDasharray}" stroke-dashoffset="${strokeDashoffset}"
      transform="rotate(-90 ${cx} ${cy})">
      <title>${esc(slice.label)}: ${slice.value} (${(percent * 100).toFixed(1)}%)</title>
    </circle>`;
  }).join('');

  const legend = slices.map(s => `
    <div class="donut-legend-item">
      <span class="donut-legend-dot" style="background:${s.color}"></span>
      <span class="donut-legend-label">${esc(s.label)}</span>
      <strong class="donut-legend-val">${s.value}</strong>
    </div>
  `).join('');

  return `<div class="chart-box donut-box">
    <div class="chart-header"><strong>${esc(title)}</strong></div>
    <div class="donut-wrap">
      <svg width="${size}" height="${size}" viewBox="0 0 ${size}" ${size}" class="donut-svg">
        ${paths}
        <text x="${cx}" y="${cy - 3}" text-anchor="middle" fill="var(--text-primary)" font-size="13" font-weight="700" font-family="ui-monospace, monospace">${total}</text>
        <text x="${cx}" y="${cy + 12}" text-anchor="middle" fill="var(--chart-label)" font-size="9" text-transform="uppercase">Total</text>
      </svg>
      <div class="donut-legend">${legend}</div>
    </div>
  </div>`;
}
