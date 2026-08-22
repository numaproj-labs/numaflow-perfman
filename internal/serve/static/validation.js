(() => {
  const PHASES = [
    "created",
    "generating",
    "namespace_created",
    "controller_ready",
    "pipeline_ready",
    "sending",
    "draining",
    "validating",
    "passed",
  ];
  const PHASE_LABELS = {
    created: "Created",
    generating: "Generating",
    namespace_created: "Namespace",
    controller_ready: "Controller",
    pipeline_ready: "Pipeline",
    sending: "Sending",
    draining: "Draining",
    validating: "Validating",
    passed: "Passed",
    failed: "Failed",
    interrupted: "Interrupted",
    progress: "Progress",
  };
  const TERMINAL = ["passed", "failed", "interrupted"];

  const els = {
    tabs: [...document.querySelectorAll(".tab")],
    scenario: document.getElementById("validation-scenario"),
    tier: document.getElementById("validation-tier"),
    image: document.getElementById("validation-image"),
    events: document.getElementById("validation-events"),
    timeout: document.getElementById("validation-timeout"),
    seed: document.getElementById("validation-seed"),
    baseEventTime: document.getElementById("validation-base-event-time"),
    dumpFailureDB: document.getElementById("validation-dump-failure-db"),
    cli: document.getElementById("validation-cli"),
    banner: document.getElementById("validation-banner"),
    error: document.getElementById("validation-error"),
    form: document.getElementById("validation-form"),
    live: document.getElementById("validation-live"),
    done: document.getElementById("validation-done"),
    start: document.getElementById("validation-start"),
    cancel: document.getElementById("validation-cancel"),
    refresh: document.getElementById("validation-refresh"),
    newRun: document.getElementById("validation-new-run"),
    liveTitle: document.getElementById("validation-live-title"),
    liveSubtitle: document.getElementById("validation-live-subtitle"),
    liveStatus: document.getElementById("validation-live-status"),
    liveNamespace: document.getElementById("validation-live-namespace"),
    liveElapsed: document.getElementById("validation-live-elapsed"),
    liveLog: document.getElementById("validation-live-log"),
    phaseRail: document.getElementById("validation-phase-rail"),
    doneTitle: document.getElementById("validation-done-title"),
    doneSubtitle: document.getElementById("validation-done-subtitle"),
    doneSummary: document.getElementById("validation-done-summary"),
    details: document.getElementById("validation-details"),
    historyMsg: document.getElementById("validation-history-msg"),
    historyBody: document.querySelector("#validation-history-table tbody"),
  };

  if (!els.scenario) return;

  let eventSource = null;
  let pollTimer = null;
  let elapsedTimer = null;
  let liveStartedAt = null;
  let activeRun = null;

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      "\"": "&quot;",
      "'": "&#39;",
    })[ch]);
  }

  async function fetchJSON(url, options) {
    const res = await fetch(url, options);
    const text = await res.text();
    let data = null;
    if (text) {
      try { data = JSON.parse(text); } catch (_) { /* plain text */ }
    }
    if (!res.ok) {
      const err = new Error((data && (data.message || data.error)) || text || res.statusText);
      err.status = res.status;
      err.data = data;
      throw err;
    }
    return data;
  }

  function showBanner(el, message, kind) {
    if (!message) {
      el.hidden = true;
      el.textContent = "";
      return;
    }
    el.hidden = false;
    el.textContent = message;
    el.classList.remove("error", "warn", "ok");
    if (kind) el.classList.add(kind);
  }

  function setMode(mode) {
    els.form.classList.toggle("hidden", mode !== "form");
    els.live.classList.toggle("hidden", mode !== "live");
    els.done.classList.toggle("hidden", mode !== "done");
  }

  function formatElapsed(ms) {
    const total = Math.max(0, Math.floor(ms / 1000));
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }

  function renderPhaseRail(status) {
    const display = PHASES.filter((p) => p !== "created");
    const failed = status === "failed" || status === "interrupted";
    let activeIdx = display.indexOf(status);
    if (failed) {
      activeIdx = Math.max(0, display.indexOf(activeRun?.lastPhase || "validating"));
    }
    if (status === "passed") activeIdx = display.length - 1;
    els.phaseRail.replaceChildren();
    display.forEach((phase, idx) => {
      const li = document.createElement("li");
      li.textContent = PHASE_LABELS[phase] || phase;
      if (failed && idx === activeIdx) li.classList.add("failed", "active");
      else if (!failed && idx < activeIdx) li.classList.add("done");
      else if (!failed && idx === activeIdx) li.classList.add("active");
      else if (status === "passed") li.classList.add("done");
      els.phaseRail.appendChild(li);
    });
  }

  function appendLog(ev) {
    const line = document.createElement("div");
    line.className = "log-line" + (ev.level === "error" ? " error" : "");
    const ts = (ev.timestamp || "").slice(11, 19) || "--:--:--";
    line.innerHTML = `<span>${escapeHTML(ts)}</span><span class="phase">${escapeHTML(PHASE_LABELS[ev.phase] || ev.phase || "")}</span><span class="msg">${escapeHTML(ev.message || "")}</span>`;
    els.liveLog.appendChild(line);
    els.liveLog.scrollTop = els.liveLog.scrollHeight;
    if (ev.phase && ev.phase !== "progress") {
      if (!activeRun) activeRun = {};
      activeRun.lastPhase = ev.phase;
      renderPhaseRail(ev.phase);
    }
  }

  function stopWatchers() {
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    if (elapsedTimer) {
      clearInterval(elapsedTimer);
      elapsedTimer = null;
    }
  }

  function updateCliPreview() {
    const parts = [
      "perfman validation run",
      `--scenario ${els.scenario.value || "<scenario>"}`,
      `--image ${els.image.value || "<image>"}`,
      `--tier ${els.tier.value || "standard"}`,
      `--timeout ${els.timeout.value || "90m"}`,
    ];
    if (els.events.value.trim() !== "") parts.push(`--events ${els.events.value.trim()}`);
    if (els.seed.value.trim() !== "") parts.push(`--seed ${els.seed.value.trim()}`);
    if (els.baseEventTime.value.trim() !== "") parts.push(`--base-event-time ${els.baseEventTime.value.trim()}`);
    if (els.dumpFailureDB.checked) parts.push("--dump-failure-db");
    els.cli.textContent = parts.join(" ");
  }

  function validationSummaryText(info) {
    const v = info.validation;
    if (!v) return info.error_message || `Validation ended with status ${info.status}.`;
    const counts = [
      ["missing", v.missing_count],
      ["unexpected", v.unexpected_count],
      ["corrupted", v.corrupted_count],
      ["routing mismatch", v.routing_mismatch_count],
      ["child mismatch", v.child_mismatch_count],
      ["duplicates", v.duplicate_delivery_count],
    ].filter(([, count]) => count > 0).map(([label, count]) => `${label}: ${count}`);
    if (v.passed) {
      return `Passed ${v.expected_count} expected outputs with ${v.physical_delivery_count} physical deliveries.`;
    }
    return counts.length ? `Failed correctness checks (${counts.join(", ")}).` : (info.error_message || "Validation failed.");
  }

  function addStat(parent, label, value) {
    const card = document.createElement("div");
    card.className = "stat";
    card.innerHTML = `<div class="stat-value">${escapeHTML(value)}</div><div class="stat-label">${escapeHTML(label)}</div>`;
    parent.appendChild(card);
  }

  function addSample(parent, sample) {
    const card = document.createElement("div");
    card.className = "sample-card";
    const title = document.createElement("h4");
    title.textContent = `Sample ${sample.ordinal}: ${sample.kind}`;
    card.appendChild(title);
    const dl = document.createElement("dl");
    [
      ["Key", sample.logical_key],
      ["Expected", sample.expected],
      ["Actual", sample.actual],
      ["Detail", sample.detail],
    ].forEach(([label, value]) => {
      if (!value) return;
      const dt = document.createElement("dt");
      dt.textContent = label;
      const dd = document.createElement("dd");
      dd.textContent = value;
      dl.appendChild(dt);
      dl.appendChild(dd);
    });
    card.appendChild(dl);
    parent.appendChild(card);
  }

  function renderDetails(info) {
    els.details.replaceChildren();
    const v = info.validation;
    if (!v) {
      if (info.error_message) {
        const div = document.createElement("div");
        div.className = "banner error";
        div.textContent = info.error_message;
        els.details.appendChild(div);
      }
      return;
    }
    const summary = document.createElement("div");
    summary.className = "summary-grid";
    addStat(summary, "Expected", v.expected_count);
    addStat(summary, "Actual logical", v.logical_output_count);
    addStat(summary, "Physical deliveries", v.physical_delivery_count);
    addStat(summary, "Missing", v.missing_count);
    addStat(summary, "Unexpected", v.unexpected_count);
    addStat(summary, "Corrupted", v.corrupted_count);
    addStat(summary, "Routing mismatch", v.routing_mismatch_count);
    addStat(summary, "Child mismatch", v.child_mismatch_count);
    addStat(summary, "Duplicates", v.duplicate_delivery_count);
    els.details.appendChild(summary);

    if ((info.failure_samples || []).length > 0) {
      const title = document.createElement("h3");
      title.textContent = "Failure samples";
      els.details.appendChild(title);
      const list = document.createElement("div");
      list.className = "sample-list";
      info.failure_samples.forEach((sample) => addSample(list, sample));
      els.details.appendChild(list);
    }
  }

  async function refreshLiveStatus(runID) {
    const info = await fetchJSON(`/api/validation-runs/${encodeURIComponent(runID)}`);
    els.liveStatus.textContent = info.status || "—";
    els.liveNamespace.textContent = info.namespace || "—";
    els.liveSubtitle.textContent = `${info.scenario} · ${info.image_ref}`;
    if (info.status && info.status !== "progress") {
      if (!activeRun) activeRun = { id: runID };
      activeRun.lastPhase = info.status;
      renderPhaseRail(info.status);
    }
    if (TERMINAL.includes(info.status) && !info.active) {
      finishRun(info);
    }
    return info;
  }

  function finishRun(info) {
    stopWatchers();
    setMode("done");
    const ok = info.status === "passed";
    els.doneTitle.textContent = ok ? "Validation passed" : `Validation ${info.status}`;
    els.doneSubtitle.textContent = `${info.scenario} · ${info.image_ref}`;
    showBanner(els.doneSummary, validationSummaryText(info), ok ? "ok" : "error");
    renderDetails(info);
    activeRun = info;
    refreshHistory().catch(() => {});
  }

  function startLiveWatch(runID) {
    stopWatchers();
    liveStartedAt = Date.now();
    els.liveLog.replaceChildren();
    elapsedTimer = setInterval(() => {
      els.liveElapsed.textContent = formatElapsed(Date.now() - liveStartedAt);
    }, 1000);
    els.liveElapsed.textContent = "00:00";

    const url = `/api/validation-runs/${encodeURIComponent(runID)}/events?after_id=0`;
    if (window.EventSource) {
      eventSource = new EventSource(url);
      eventSource.onmessage = (msg) => {
        try {
          const ev = JSON.parse(msg.data);
          appendLog(ev);
          if (TERMINAL.includes(ev.phase)) {
            refreshLiveStatus(runID).catch(() => {});
          }
        } catch (_) { /* ignore */ }
      };
      eventSource.onerror = () => {
        if (eventSource) {
          eventSource.close();
          eventSource = null;
        }
        if (!pollTimer) {
          pollTimer = setInterval(() => {
            refreshLiveStatus(runID).catch(() => {});
            fetchJSON(url).then((data) => {
              for (const ev of data.events || []) appendLog(ev);
            }).catch(() => {});
          }, 3000);
        }
      };
    } else {
      pollTimer = setInterval(() => {
        refreshLiveStatus(runID).catch(() => {});
      }, 2000);
    }
    refreshLiveStatus(runID).catch(() => {});
  }

  function requestBody() {
    const body = {
      scenario: els.scenario.value,
      image: els.image.value.trim(),
      tier: els.tier.value,
      timeout: els.timeout.value.trim() || "90m",
      dump_failure_db: els.dumpFailureDB.checked,
    };
    if (els.events.value.trim() !== "") body.events = Number(els.events.value.trim());
    if (els.seed.value.trim() !== "") body.seed = els.seed.value.trim();
    if (els.baseEventTime.value.trim() !== "") body.base_event_time = els.baseEventTime.value.trim();
    return body;
  }

  async function startRun() {
    showBanner(els.error, "", null);
    els.start.disabled = true;
    try {
      const info = await fetchJSON("/api/validation-runs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(requestBody()),
      });
      activeRun = info;
      setMode("live");
      els.liveTitle.textContent = "Live validation";
      renderPhaseRail(info.status || "created");
      startLiveWatch(info.id);
      if (info.mutable_tag_warning) {
        appendLog({
          timestamp: new Date().toISOString(),
          level: "info",
          phase: "progress",
          message: "warning: image uses a mutable tag; prefer digests for reproducible validation",
        });
      }
    } catch (err) {
      if (err.status === 409) {
        showBanner(els.error, err.data?.message || err.message || "Validation is already active", "error");
      } else {
        showBanner(els.error, err.data?.message || err.message || "Failed to start validation", "error");
      }
    } finally {
      els.start.disabled = false;
    }
  }

  async function loadConfig() {
    try {
      const config = await fetchJSON("/api/config");
      if (config.image && !els.image.value) els.image.value = config.image;
      if (!config.udf_image) {
        showBanner(els.banner, "UDF image is not configured. Run setup or: perfman config set --key udf_image --value <image>", "warn");
      } else if (!config.execution_enabled) {
        showBanner(els.banner, "Validation execution is not enabled on this server.", "warn");
      } else {
        showBanner(els.banner, "", null);
      }
    } catch (_) {
      showBanner(els.banner, "Unable to load server config.", "warn");
    }
  }

  async function loadValidations() {
    const data = await fetchJSON("/api/validations");
    const validations = data.validations || [];
    els.scenario.replaceChildren();
    for (const v of validations) {
      const opt = document.createElement("option");
      opt.value = v.id;
      opt.textContent = v.description ? `${v.id} — ${v.description}` : v.id;
      els.scenario.appendChild(opt);
    }
    els.scenario.value = data.default || validations[0]?.id || "";
    updateCliPreview();
  }

  async function refreshHistory() {
    els.historyMsg.textContent = "Loading…";
    try {
      const data = await fetchJSON("/api/validation-runs?limit=100");
      const runs = data.runs || [];
      els.historyBody.replaceChildren();
      for (const run of runs) {
        const tr = document.createElement("tr");
        tr.innerHTML = `
          <td>${escapeHTML(run.created_at || "—")}</td>
          <td>${escapeHTML(run.scenario || "")}</td>
          <td>${escapeHTML(run.image_ref || "")}</td>
          <td><span class="pill ${escapeHTML(run.status || "")}">${escapeHTML(run.status || "")}</span></td>
          <td>${escapeHTML(run.namespace || "")}</td>
          <td></td>
        `;
        const actions = tr.lastElementChild;
        if (run.active || !TERMINAL.includes(run.status)) {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.className = "btn secondary";
          btn.textContent = "Watch";
          btn.addEventListener("click", () => {
            activeRun = run;
            setMode("live");
            renderPhaseRail(run.status || "created");
            startLiveWatch(run.id);
          });
          actions.appendChild(btn);
        } else {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.className = "btn secondary";
          btn.textContent = run.status === "failed" ? "Details" : "Open";
          btn.addEventListener("click", async () => {
            const info = await fetchJSON(`/api/validation-runs/${encodeURIComponent(run.id)}`);
            activeRun = info;
            finishRun(info);
          });
          actions.appendChild(btn);
        }
        els.historyBody.appendChild(tr);
      }
      els.historyMsg.textContent = runs.length ? `${runs.length} validation run(s)` : "No validation runs found.";
    } catch (err) {
      els.historyMsg.textContent = err.message || "Failed to load validation history";
      els.historyMsg.classList.add("error");
    }
  }

  ["change", "input"].forEach((evt) => {
    els.scenario.addEventListener(evt, updateCliPreview);
    els.tier.addEventListener(evt, updateCliPreview);
    els.image.addEventListener(evt, updateCliPreview);
    els.events.addEventListener(evt, updateCliPreview);
    els.timeout.addEventListener(evt, updateCliPreview);
    els.seed.addEventListener(evt, updateCliPreview);
    els.baseEventTime.addEventListener(evt, updateCliPreview);
    els.dumpFailureDB.addEventListener(evt, updateCliPreview);
  });
  els.start.addEventListener("click", startRun);
  els.refresh.addEventListener("click", refreshHistory);
  els.cancel.addEventListener("click", async () => {
    if (!activeRun?.id) return;
    try {
      await fetchJSON(`/api/validation-runs/${encodeURIComponent(activeRun.id)}/cancel`, { method: "POST" });
      appendLog({
        timestamp: new Date().toISOString(),
        level: "info",
        phase: "progress",
        message: "cancellation requested",
      });
    } catch (err) {
      showBanner(els.error, err.message || "Cancel failed", "error");
    }
  });
  els.newRun.addEventListener("click", () => {
    activeRun = null;
    showBanner(els.error, "", null);
    setMode("form");
  });
  els.tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      if (tab.dataset.view === "validation") refreshHistory().catch(() => {});
    });
  });

  (async () => {
    try {
      await loadConfig();
      await loadValidations();
      await refreshHistory();
      setMode("form");
    } catch (err) {
      showBanner(els.error, err.message || "Failed to initialize validation UI", "error");
    }
  })();
})();
