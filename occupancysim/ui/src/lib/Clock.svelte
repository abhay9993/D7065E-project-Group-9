<script>
  import { api } from './api.js';

  let { clock, buildsim, levels = [], streaming } = $props();

  const presets = [1, 10, 60, 300, 900];
  let jumpTime = $state('');
  let jumpDate = $state('');
  let error = $state('');

  async function send(change) {
    error = '';
    try { await api.clock(change); } catch (e) { error = e.message; }
  }

  function jump() {
    const change = {};
    if (jumpTime) change.time = jumpTime;
    if (jumpDate) change.date = jumpDate;
    if (Object.keys(change).length) send(change);
  }

  function nextWeekday(target) {
    // target: 1 = Monday … 6 = Saturday
    const now = new Date(clock.time);
    const delta = ((target - now.getDay()) + 7) % 7 || 7;
    now.setDate(now.getDate() + delta);
    return now.toISOString().slice(0, 10);
  }

  const viewerURL = (level) => `${buildsim.public_url.replace(/\/$/, '')}/?floor=${level}`;
</script>

<div class="clock">
  {clock.time_of_day}
  <small>{clock.weekday} {clock.date}{clock.weekend ? ' · weekend' : ''} · {clock.factor}× {clock.running ? '' : '· paused'}</small>
</div>

<div class="controls">
  <button class:on={clock.running} onclick={() => send({ running: !clock.running })}>
    {clock.running ? 'Pause' : 'Run'}
  </button>
  {#each presets as factor}
    <button class:on={clock.factor === factor} onclick={() => send({ factor })}>{factor}×</button>
  {/each}
  <input type="number" min="1" max="3600" placeholder="custom ×" onchange={(e) => e.target.value && send({ factor: Number(e.target.value) })} />
</div>

<div class="controls">
  <input type="time" bind:value={jumpTime} />
  <input type="date" bind:value={jumpDate} style="width:140px" />
  <button onclick={jump}>Jump</button>
  <button onclick={() => send({ date: nextWeekday(1), time: '07:30' })}>Next Monday</button>
  <button onclick={() => send({ date: nextWeekday(6), time: '10:00' })}>Next Saturday</button>
</div>

<div class="controls" style="margin-left:auto">
  <span title={buildsim.last_error || ''}>
    <span class="dot" class:ok={buildsim.connected}></span>
    BuildSim {buildsim.connected ? `${buildsim.entities} entities` : 'unreachable'}
  </span>
  <span><span class="dot" class:ok={streaming}></span>live</span>
  {#each levels as level}
    <a href={viewerURL(level)} target="_blank" rel="noopener">{level} ↗</a>
  {/each}
</div>
{#if error}<div class="error">{error}</div>{/if}
