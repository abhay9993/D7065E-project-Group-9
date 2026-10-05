<script>
  let { people = [], total = 0, levels = [] } = $props();
  let onlyInside = $state(true);
  let visible = $derived(people.filter((p) => !onlyInside || p.state !== 'outside').slice(0, 150));
</script>

<div class="chips">
  <button class:on={onlyInside} onclick={() => (onlyInside = true)}>Inside</button>
  <button class:on={!onlyInside} onclick={() => (onlyInside = false)}>Everyone (first {Math.min(people.length, 150)} of {total})</button>
</div>
<div class="scroll">
  <table>
    <thead><tr><th>Name</th><th>Role</th><th>Office</th><th>Where</th><th>Status</th></tr></thead>
    <tbody>
      {#each visible as p (p.id)}
        <tr>
          <td>{p.name}</td>
          <td><span class="role {p.role}">{p.role}</span></td>
          {#if levels.length > 1}<td>{p.level}</td>{/if}
          <td>{p.office || '—'}</td>
          <td>{p.room || 'outside'}</td>
          <td>{p.status.replace(/^[a-z]+: /, '')}</td>
        </tr>
      {:else}
        <tr><td colspan={levels.length > 1 ? 6 : 5} class="hint">Nobody is inside right now.</td></tr>
      {/each}
    </tbody>
  </table>
</div>
