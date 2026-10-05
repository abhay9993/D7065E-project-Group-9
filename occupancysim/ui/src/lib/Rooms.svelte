<script>
  let { rooms = [], entrances = [], levels = [] } = $props();
  let filter = $state('used');
  let level = $state('all');
  const filters = [
    ['used', 'Occupied'], ['office', 'Offices'], ['lecture', 'Lecture rooms'], ['fika', 'Fika rooms'], ['corridor', 'Corridors'], ['all', 'All'],
  ];
  let onFloor = $derived(rooms.filter((r) => level === 'all' || r.level === level));
  let visible = $derived(
    onFloor
      .filter((r) => filter === 'all' || (filter === 'used' ? r.occupants > 0 : r.role === filter))
      .sort((a, b) => b.occupants - a.occupants || a.name.localeCompare(b.name))
      .slice(0, 120)
  );
  let summary = $derived.by(() => {
    const out = {};
    for (const r of onFloor) {
      out[r.role] ??= { rooms: 0, occupants: 0 };
      out[r.role].rooms++;
      out[r.role].occupants += r.occupants;
    }
    return out;
  });
  let maxOcc = $derived(Math.max(1, ...visible.map((r) => r.occupants)));
</script>

{#if levels.length > 1}
  <div class="chips">
    <button class:on={level === 'all'} onclick={() => (level = 'all')}>All floors</button>
    {#each levels as l}
      <button class:on={level === l} onclick={() => (level = l)}>{l}</button>
    {/each}
  </div>
{/if}
<div class="chips">
  {#each filters as [key, label]}
    <button class:on={filter === key} onclick={() => (filter = key)}>
      {label}{summary[key] ? ` (${summary[key].rooms})` : ''}
    </button>
  {/each}
</div>
<p class="hint">
  Entrances: {#each entrances as e, i}{i ? ', ' : ''}<strong title="({e.position[0]}, {e.position[1]})">{e.name}</strong> → {e.level}/{e.node}{/each}.
  People enter through the entrance nearest their first room and leave through the one nearest their last; a floor
  without configured entrances uses its largest corridor.
  Roles follow the area rules in the parameters; override a room under “Role overrides”.
</p>
<div class="scroll">
  <table>
    <thead><tr><th>Room</th>{#if levels.length > 1}<th>Floor</th>{/if}<th>Role</th><th class="num">m²</th><th class="num">Capacity</th><th class="num">People</th><th></th></tr></thead>
    <tbody>
      {#each visible as r (r.level + '/' + r.name)}
        <tr>
          <td>{r.name}</td>
          {#if levels.length > 1}<td>{r.level}</td>{/if}
          <td><span class="role {r.role}">{r.role}</span></td>
          <td class="num">{r.area_m2.toFixed(0)}</td>
          <td class="num">{r.capacity || ''}</td>
          <td class="num">{r.occupants}</td>
          <td><div class="bar"><div style="width:{(100 * r.occupants) / maxOcc}%"></div></div></td>
        </tr>
      {:else}
        <tr><td colspan={levels.length > 1 ? 7 : 6} class="hint">No rooms match.</td></tr>
      {/each}
    </tbody>
  </table>
</div>
