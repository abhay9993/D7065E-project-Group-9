<script>
  import { onMount } from 'svelte';
  import { subscribe } from './lib/api.js';
  import Clock from './lib/Clock.svelte';
  import Parameters from './lib/Parameters.svelte';
  import Chart from './lib/Chart.svelte';
  import Rooms from './lib/Rooms.svelte';
  import Lectures from './lib/Lectures.svelte';
  import People from './lib/People.svelte';

  let state = $state(null);
  let streaming = $state(false);

  onMount(() => subscribe((next) => { state = next; }, (ok) => { streaming = ok; }));

  const roleOrder = ['staff', 'lecturer', 'student', 'guard'];
</script>

<div class="app">
  <header>
    <div>
      <h1>Occupancy simulator</h1>
      <div class="hint">People movement for BuildSim {(state?.sim.levels ?? []).length === 1 ? 'floor' : 'floors'} <strong>{(state?.sim.levels ?? ['…']).join(', ')}</strong></div>
    </div>
    {#if state}
      <Clock clock={state.sim.clock} buildsim={state.buildsim} levels={state.sim.levels} {streaming} />
    {:else}
      <div class="hint">{streaming ? 'Waiting for state…' : 'Connecting to the occupancy service…'}</div>
    {/if}
  </header>

  <div class="layout">
    <aside class="stack">
      <Parameters />
    </aside>

    <main class="stack">
      {#if state}
        <section class="panel">
          <h2>Right now</h2>
          <div class="cards">
            <div class="card"><div class="value">{state.sim.counts.inside}</div><div class="label">inside of {state.sim.counts.population}</div></div>
            <div class="card"><div class="value">{state.sim.counts.walking}</div><div class="label">walking</div></div>
            <div class="card"><div class="value">{state.sim.counts.in_room}</div><div class="label">in a room</div></div>
            {#each state.sim.levels as level}
              <div class="card">
                <div class="value">{state.sim.counts.inside_by_level[level] ?? 0}</div>
                <div class="label">on {level} of {state.sim.counts.by_level[level] ?? 0}</div>
              </div>
            {/each}
            {#each roleOrder as role}
              {#if state.sim.counts.by_role[role]}
                <div class="card">
                  <div class="value">{state.sim.counts.inside_by_role[role] ?? 0}</div>
                  <div class="label"><span class="role {role}">{role}</span> of {state.sim.counts.by_role[role]}</div>
                </div>
              {/if}
            {/each}
          </div>
        </section>

        <section class="panel">
          <h2>Today's occupancy curve</h2>
          <Chart timeline={state.sim.timeline} population={state.sim.counts.population} minute={state.sim.clock.minute_of_day} />
        </section>

        <section class="panel">
          <h2>Rooms</h2>
          <Rooms rooms={state.sim.rooms} entrances={state.sim.entrances} levels={state.sim.levels} />
        </section>

        <section class="panel">
          <h2>Lecture timetable</h2>
          <Lectures lectures={state.sim.lectures} minute={state.sim.clock.minute_of_day} levels={state.sim.levels} />
        </section>

        <section class="panel">
          <h2>People</h2>
          <People people={state.sim.people} total={state.sim.counts.population} levels={state.sim.levels} />
        </section>
      {/if}
    </main>
  </div>
</div>
