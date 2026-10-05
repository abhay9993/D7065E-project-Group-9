<script>
  import { onMount } from 'svelte';
  import { api } from './api.js';

  let form = $state(null);
  let slotsText = $state('');
  let rolesText = $state('');
  let entrancesText = $state('');
  let error = $state('');
  let message = $state('');
  let busy = $state(false);

  function load(params) {
    form = params;
    slotsText = (params.lecture_slots || []).join('\n');
    rolesText = Object.entries(params.room_roles || {}).map(([room, role]) => `${room}=${role}`).join('\n');
    entrancesText = (params.entrances || []).map((e) => `${e.level ? `${e.level}: ` : ''}${e.name} @ ${e.position[0]}, ${e.position[1]}`).join('\n');
  }

  onMount(async () => {
    try { load(await api.config()); } catch (e) { error = e.message; }
  });

  function collect() {
    const params = { ...form };
    params.lecture_slots = slotsText.split('\n').map((s) => s.trim()).filter(Boolean);
    params.room_roles = {};
    for (const line of rolesText.split('\n')) {
      const [room, role] = line.split('=').map((s) => s.trim());
      if (room && role) params.room_roles[room] = role;
    }
    params.entrances = [];
    for (const line of entrancesText.split('\n')) {
      const match = line.match(/^\s*(?:([\w-]+)\s*:\s*)?(.*?)\s*@\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*$/);
      if (match) params.entrances.push({ name: match[2], level: match[1] || '', position: [Number(match[3]), Number(match[4])] });
      else if (line.trim()) throw new Error(`Entrance line "${line.trim()}" must look like: level0: Name @ x, y`);
    }
    return params;
  }

  async function apply() {
    busy = true; error = ''; message = '';
    try {
      load(await api.saveConfig(collect()));
      message = 'Applied. The current day was regenerated with the new parameters.';
    } catch (e) { error = e.message; } finally { busy = false; }
  }

  async function resetDefaults() {
    error = ''; message = '';
    try { load(await api.defaults()); message = 'Defaults loaded; press Apply to use them.'; } catch (e) { error = e.message; }
  }
</script>

<section class="panel">
  <h2>Parameters</h2>
  {#if form}
    <h3>Population</h3>
    <div class="field"><label for="pop">People per weekday</label><input id="pop" type="number" min="0" max="1500" bind:value={form.weekday_population} /></div>
    <div class="field"><label for="wf">Weekend fraction</label><input id="wf" type="number" min="0" max="1" step="0.05" bind:value={form.weekend_fraction} /></div>
    <div class="field"><label for="ng">Night guards</label><input id="ng" type="number" min="0" max="20" bind:value={form.night_guards} /></div>
    <div class="field"><label for="ss">Student share</label><input id="ss" type="number" min="0" max="1" step="0.05" bind:value={form.student_share} /></div>
    <div class="field"><label for="ls">Lecturer share of staff</label><input id="ls" type="number" min="0" max="1" step="0.05" bind:value={form.lecturer_share} /></div>
    <p class="hint">The population is spread over every simulated floor: office workers follow the offices, students the lecture seats. Offices hold one person. Students only attend lectures; weekends have staff only.</p>

    <h3>Working day</h3>
    <div class="field"><label for="as">Arrive between</label><div style="display:flex;gap:4px"><input id="as" type="time" bind:value={form.arrive_start} /><input type="time" bind:value={form.arrive_end} /></div></div>
    <div class="field"><label for="lus">Lunch between</label><div style="display:flex;gap:4px"><input id="lus" type="time" bind:value={form.lunch_start} /><input type="time" bind:value={form.lunch_end} /></div></div>
    <div class="field"><label for="les">Leave between</label><div style="display:flex;gap:4px"><input id="les" type="time" bind:value={form.leave_start} /><input type="time" bind:value={form.leave_end} /></div></div>
    <div class="field"><label for="lm">Lunch minutes</label><input id="lm" type="number" min="10" max="120" bind:value={form.lunch_minutes} /></div>
    <div class="field"><label for="lo">Lunch outside probability</label><input id="lo" type="number" min="0" max="1" step="0.05" bind:value={form.lunch_out_probability} /></div>

    <h3>Fika and lectures</h3>
    <div class="field"><label for="fp">Fika probability (per break)</label><input id="fp" type="number" min="0" max="1" step="0.05" bind:value={form.fika_probability} /></div>
    <div class="field"><label for="fm">Fika minutes</label><input id="fm" type="number" min="5" max="60" bind:value={form.fika_minutes} /></div>
    <div class="field"><label for="l1">Lectures per student</label><div style="display:flex;gap:4px"><input id="l1" type="number" min="0" max="6" bind:value={form.lectures_per_student_min} /><input type="number" min="0" max="6" bind:value={form.lectures_per_student_max} /></div></div>
    <div class="field"><label for="lu">Lecture room utilisation</label><input id="lu" type="number" min="0" max="1" step="0.05" bind:value={form.lecture_room_utilisation} /></div>
    <div class="field"><label for="lf">Lecture fill target</label><input id="lf" type="number" min="0.05" max="1" step="0.05" bind:value={form.lecture_fill_target} /></div>
    <p class="hint">Only as many lecture rooms are booked per slot as the students fill to the target, so classes are full instead of scattered. Utilisation caps the share of a floor that may be booked at once.</p>
    <div class="field wide"><label for="slots">Lecture slots (one per line, HH:MM-HH:MM)</label><textarea id="slots" rows="4" bind:value={slotsText}></textarea></div>

    <h3>Room classification</h3>
    <div class="field"><label for="mpu">Metres per plan unit</label><input id="mpu" type="number" min="0.05" max="5" step="0.05" bind:value={form.metres_per_unit} /></div>
    <div class="field"><label for="mr">Ignore rooms below (m²)</label><input id="mr" type="number" min="0" bind:value={form.min_room_m2} /></div>
    <div class="field"><label for="om">Office up to (m²)</label><input id="om" type="number" min="1" bind:value={form.office_max_m2} /></div>
    <div class="field"><label for="lmin">Lecture room from (m²)</label><input id="lmin" type="number" min="1" bind:value={form.lecture_min_m2} /></div>
    <div class="field"><label for="fmin">Fika room from (m²)</label><input id="fmin" type="number" min="1" bind:value={form.fika_min_m2} /></div>
    <div class="field"><label for="fc">Number of fika rooms</label><input id="fc" type="number" min="0" max="20" bind:value={form.fika_room_count} /></div>
    <div class="field wide"><label for="ent">Entrances (one per line, [level:] Name @ x, y in plan coordinates; a floor with none uses its largest corridor)</label><textarea id="ent" rows="5" bind:value={entrancesText}></textarea></div>
    <div class="field wide"><label for="roles">Role overrides (one per line, ROOM=office|lecture|fika|unused)</label><textarea id="roles" rows="3" bind:value={rolesText}></textarea></div>

    <h3>Movement and publishing</h3>
    <div class="field"><label for="ws">Walking speed (m/s)</label><input id="ws" type="number" min="0.1" max="10" step="0.1" bind:value={form.walking_speed_mps} /></div>
    <div class="field"><label for="pi">Publish interval (ms)</label><input id="pi" type="number" min="200" max="10000" step="100" bind:value={form.publish_interval_ms} /></div>
    <div class="field"><label for="oi">Occupancy interval (ms)</label><input id="oi" type="number" min="200" max="60000" step="100" bind:value={form.occupancy_interval_ms} /></div>
    <p class="hint">Entity positions go to BuildSim on every publish interval and the viewer interpolates between two of them. Room occupancy is written at most once per occupancy interval, and then only when it differs from what BuildSim already has.</p>
    <div class="field"><label for="seed">Random seed</label><input id="seed" type="number" bind:value={form.seed} /></div>

    <div class="controls" style="margin-top:12px">
      <button class="primary" onclick={apply} disabled={busy}>Apply</button>
      <button onclick={resetDefaults} disabled={busy}>Defaults</button>
    </div>
    {#if error}<div class="error">{error}</div>{/if}
    {#if message}<div class="ok-msg">{message}</div>{/if}
  {:else if error}
    <div class="error">{error}</div>
  {:else}
    <div class="hint">Loading…</div>
  {/if}
</section>
