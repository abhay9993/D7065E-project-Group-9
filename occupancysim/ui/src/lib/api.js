// Thin wrappers around the occupancy service's JSON API.

async function request(method, path, body) {
  const response = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { error: text }; }
  if (!response.ok) throw new Error(data?.error || `HTTP ${response.status}`);
  return data;
}

export const api = {
  state: () => request('GET', '/api/state'),
  config: () => request('GET', '/api/config'),
  defaults: () => request('GET', '/api/defaults'),
  saveConfig: (params) => request('PUT', '/api/config', params),
  clock: (change) => request('POST', '/api/clock', change),
};

// subscribe opens the server-sent event stream. The browser reconnects on its
// own after a dropped connection; the callbacks only track the status.
export function subscribe(onState, onStatus) {
  const source = new EventSource('/api/events');
  source.onmessage = (event) => {
    try { onState(JSON.parse(event.data)); onStatus(true); } catch (error) { console.warn(error); }
  };
  source.onerror = () => onStatus(false);
  return () => source.close();
}
