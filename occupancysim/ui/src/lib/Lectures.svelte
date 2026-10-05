<script>
  let { lectures = [], minute = 0, levels = [] } = $props();
  const clock = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(Math.round(m % 60)).padStart(2, '0')}`;
</script>

{#if lectures.length === 0}
  <p class="hint">No lectures today.</p>
{:else}
  <div class="scroll">
    <table>
      <thead><tr><th>Slot</th>{#if levels.length > 1}<th>Floor</th>{/if}<th>Room</th><th>Lecturer</th><th class="num">Students</th><th class="num">Seats</th></tr></thead>
      <tbody>
        {#each lectures as l}
          <tr style:font-weight={minute >= l.start && minute < l.end ? 700 : 400}>
            <td>{clock(l.start)}–{clock(l.end)}</td>
            {#if levels.length > 1}<td>{l.level}</td>{/if}
            <td>{l.room}</td>
            <td>{l.lecturer || '—'}</td>
            <td class="num">{l.students}</td>
            <td class="num">{l.seats}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
