<script>
  let { timeline = [], population = 0, minute = 0 } = $props();

  const W = 720, H = 200, padL = 34, padR = 10, padT = 10, padB = 22;
  const series = [
    { key: 'inside', label: 'inside', color: 'var(--total)' },
    { key: 'staff', label: 'staff', color: 'var(--staff)' },
    { key: 'lecturers', label: 'lecturers', color: 'var(--lecturer)' },
    { key: 'students', label: 'students', color: 'var(--student)' },
    { key: 'guards', label: 'guards', color: 'var(--guard)' },
  ];

  let maxY = $derived(Math.max(10, population, ...timeline.map((s) => s.inside)));
  const x = (m) => padL + (m / 1440) * (W - padL - padR);
  let y = $derived((v) => H - padB - (v / maxY) * (H - padT - padB));

  let paths = $derived(series.map((s) => ({
    ...s,
    points: timeline.map((sample) => `${x(sample.minute).toFixed(1)},${y(sample[s.key]).toFixed(1)}`).join(' '),
  })));
  const hours = [0, 3, 6, 9, 12, 15, 18, 21, 24];
</script>

<svg viewBox="0 0 {W} {H}" width="100%" role="img" aria-label="People inside the building over the simulated day">
  {#each hours as h}
    <line x1={x(h * 60)} x2={x(h * 60)} y1={padT} y2={H - padB} stroke="var(--line)" />
    <text x={x(h * 60)} y={H - 6} font-size="10" fill="var(--muted)" text-anchor="middle">{String(h).padStart(2, '0')}:00</text>
  {/each}
  {#each [0, 0.5, 1] as f}
    <line x1={padL} x2={W - padR} y1={y(f * maxY)} y2={y(f * maxY)} stroke="var(--line)" />
    <text x={padL - 4} y={y(f * maxY) + 3} font-size="10" fill="var(--muted)" text-anchor="end">{Math.round(f * maxY)}</text>
  {/each}
  {#each paths as p}
    {#if timeline.length > 1}
      <polyline points={p.points} fill="none" stroke={p.color} stroke-width={p.key === 'inside' ? 2 : 1.2} />
    {/if}
  {/each}
  <line x1={x(minute)} x2={x(minute)} y1={padT} y2={H - padB} stroke="var(--accent)" stroke-dasharray="3 3" />
</svg>
<div class="legend">
  {#each series as s}<span style="--c:{s.color}">{s.label}</span>{/each}
</div>
