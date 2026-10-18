"use strict";
const el = id => document.getElementById(id);
const state = { entries: [], next: 0, paused: false, busy: false, generation: 0, controller: null, timer: null, searchTimer: null, composing: false, sources: new Map([["live", { source: "live" }]]) };
const dateFormat = new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
function textNode(tag, text, className) {
  const node = document.createElement(tag);
  node.textContent = text;
  if (className) node.className = className;
  return node;
}
function setPaused(paused) {
  state.paused = paused;
  el("pause").textContent = paused ? "Resume updates" : "Pause updates";
  el("pause").setAttribute("aria-pressed", String(paused));
  el("mode").textContent = paused ? "Updates paused" : "Live updates · every 3s";
  el("mode").classList.toggle("paused", paused);
  clearTimeout(state.timer);
  if (paused) {
    state.generation++;
    state.controller?.abort();
    state.busy = false;
    document.querySelector(".feed").setAttribute("aria-busy", "false");
    el("older").disabled = !state.next || state.entries.length >= 1000;
  }
}
function schedule() {
  clearTimeout(state.timer);
  if (!state.paused && !document.hidden) state.timer = setTimeout(() => load(false), 3000);
}
async function fetchJSON(path, options = {}) {
  const response = await fetch(path, { credentials: "same-origin", cache: "no-store", ...options });
  if (!response.ok) {
    const messages = {401: "Sign-in required. Reload this page to authenticate.", 403: "Access denied. Check the viewer credentials and file permissions.", 404: "This log file is no longer available. Refresh to update the sources.", 409: "This file changed while browsing. Refresh to read its latest entries.", 429: "The viewer is busy. Retry in a moment."};
    throw new Error(messages[response.status] || "Logs could not be loaded. Check the connection and refresh to retry.");
  }
  return response.json();
}
async function sources() {
  const data = await fetchJSON("./sources", { signal: AbortSignal.timeout(10000) });
  const selected = el("source").value;
  const choices = new Map([["live", { source: "live" }]]);
  const fragment = document.createDocumentFragment();
  const add = (key, label, query) => {
    choices.set(key, query);
    const option = textNode("option", label); option.value = key; fragment.append(option);
  };
  add("live", "Live · all local channels", { source: "live" });
  for (const source of data.sources) {
    add("live:" + source.name, "Live · " + source.name, { source: "live", channel: source.name });
    for (const file of source.files || []) {
      add("file:" + source.name + ":" + file.date, source.name + " / " + file.name, { source: "file", channel: source.name, date: file.date });
    }
  }
  state.sources = choices;
  el("source").replaceChildren(fragment);
  el("source").value = choices.has(selected) ? selected : "live";
  el("retention").textContent = "Live buffer · up to " + data.capacity.toLocaleString() + " entries / " + (data.bytes / 1048576).toLocaleString() + " MiB";
}
function render() {
  const fragment = document.createDocumentFragment();
  for (const entry of state.entries) {
    const details = document.createElement("details");
    const summary = document.createElement("summary");
    const date = new Date(entry.time);
    const time = textNode("span", entry.time && !Number.isNaN(date.getTime()) ? dateFormat.format(date) : "—", "time");
    time.title = entry.time || "Timestamp is part of the saved text";
    const level = textNode("span", entry.level || "TEXT", "level");
    if (["DEBUG", "INFO", "WARN", "ERROR"].includes(entry.level)) level.classList.add(entry.level);
    summary.append(time, level, textNode("span", entry.channel, "channel"), textNode("span", entry.message || "(empty message)", "message"));
    const detail = document.createElement("div"); detail.className = "detail";
    const actions = document.createElement("div"); actions.className = "detail-actions";
    const copy = textNode("button", "Copy entry"); copy.type = "button";
    copy.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(entry.raw); el("status").textContent = "Entry copied."; }
      catch { el("status").textContent = "Copy is unavailable. Select and copy the entry text below."; }
    });
    actions.append(copy);
    if (entry.truncated) actions.append(textNode("span", "Preview truncated at 16 KiB."));
    let formatted = entry.raw;
    try { formatted = JSON.stringify(JSON.parse(entry.raw), null, 2); } catch { /* Text logs keep their original content. */ }
    detail.append(actions, textNode("pre", formatted));
    details.append(summary, detail);
    details.addEventListener("toggle", () => { if (details.open) setPaused(true); });
    fragment.append(details);
  }
  el("entries").replaceChildren(fragment);
  el("empty").hidden = state.entries.length !== 0;
  el("empty").textContent = el("search").value || el("level").value ? "No matching entries in this window. Change the filters or load older logs." : "No entries yet. Generate application activity or choose a saved file.";
  el("count").textContent = state.entries.length.toLocaleString() + " entries";
  el("export").disabled = state.entries.length === 0;
  el("older").disabled = !state.next || state.entries.length >= 1000;
}
async function load(older) {
  const generation = ++state.generation;
  state.controller?.abort();
  const controller = new AbortController(); state.controller = controller;
  clearTimeout(state.timer);
  state.busy = true;
  document.querySelector(".feed").setAttribute("aria-busy", "true");
  el("older").disabled = true;
  el("error").hidden = true;
  el("status").textContent = older ? "Loading older entries…" : "Refreshing…";
  const timeout = setTimeout(() => controller.abort("timeout"), 10000);
  try {
    const query = { ...(state.sources.get(el("source").value) || { source: "live" }), level: el("level").value, search: el("search").value, limit: 200, before: older ? state.next : 0 };
    const result = await fetchJSON("./query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(query), signal: controller.signal });
    if (generation !== state.generation) return;
    state.entries = older ? state.entries.concat(result.entries).slice(0, 1000) : result.entries;
    state.next = result.next;
    render();
    el("pause").disabled = false;
    const notes = ["Updated " + new Date().toLocaleTimeString()];
    if (result.limited) notes.push("File scan limited to 1 MiB; load older to continue");
    if (result.evicted) notes.push(result.evicted.toLocaleString() + " older live entries expired");
    if (state.entries.length >= 1000) notes.push("Display limit reached; export these entries or refine your search");
    el("status").textContent = notes.join(" · ");
  } catch (error) {
    if (generation !== state.generation) return;
    state.controller = null;
    setPaused(true);
    el("error").textContent = controller.signal.aborted ? "The request timed out. Refresh to retry." : error.message;
    el("error").hidden = false;
    el("status").textContent = "Updates stopped. Existing entries remain available.";
    if (!state.entries.length) el("empty").textContent = "Logs are unavailable. Refresh to retry.";
  } finally {
    clearTimeout(timeout);
    if (generation === state.generation) {
      state.busy = false;
      document.querySelector(".feed").setAttribute("aria-busy", "false");
      el("older").disabled = !state.next || state.entries.length >= 1000;
      schedule();
    }
  }
}
async function refresh() {
  if (el("refresh").disabled) return;
  el("refresh").disabled = true;
  try { await sources(); await load(false); }
  catch (error) { setPaused(true); el("error").textContent = "Sources could not be loaded. Check the connection and configured log directory, then refresh."; el("error").hidden = false; el("status").textContent = "Refresh to retry."; if (!state.entries.length) el("empty").textContent = "Logs are unavailable. Refresh to retry."; }
  finally { el("refresh").disabled = false; }
}
el("pause").addEventListener("click", () => { setPaused(!state.paused); if (!state.paused) load(false); });
el("refresh").addEventListener("click", refresh);
el("older").addEventListener("click", () => { if (state.busy) return; setPaused(true); load(true); });
for (const id of ["source", "level"]) el(id).addEventListener("change", () => load(false));
function search() { el("clear").hidden = !el("search").value; el("clear").disabled = !el("search").value; clearTimeout(state.searchTimer); clearTimeout(state.timer); state.generation++; state.controller?.abort(); if (!state.composing) state.searchTimer = setTimeout(() => load(false), 300); }
el("search").addEventListener("input", search);
el("search").addEventListener("compositionstart", () => { state.composing = true; clearTimeout(state.searchTimer); });
el("search").addEventListener("compositionend", () => { state.composing = false; search(); });
el("search").addEventListener("keydown", event => { if (event.key === "Enter" && !event.isComposing) { clearTimeout(state.searchTimer); load(false); } });
el("clear").addEventListener("click", () => { el("search").value = ""; el("clear").hidden = true; el("clear").disabled = true; clearTimeout(state.searchTimer); el("search").focus(); load(false); });
el("export").addEventListener("click", () => {
  const blob = new Blob([state.entries.map(entry => JSON.stringify(entry)).join("\n") + "\n"], { type: "application/x-ndjson" });
  const url = URL.createObjectURL(blob); const link = document.createElement("a"); link.href = url; link.download = "logs-" + new Date().toISOString().replaceAll(":", "-") + ".jsonl"; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  el("status").textContent = "Exported " + state.entries.length + " visible entries.";
});
document.addEventListener("visibilitychange", () => { clearTimeout(state.timer); if (!document.hidden && !state.paused) load(false); });
// Enable controls only after their behavior is attached. With scripts disabled,
// the static document does not present actions that cannot work.
el("refresh").disabled = false;
refresh();
