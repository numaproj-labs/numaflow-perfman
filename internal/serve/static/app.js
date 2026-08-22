(() => {
  const PHASES = [
    "created",
    "preflight",
    "namespace_created",
    "controller_ready",
    "pipeline_ready",
    "measuring",
    "collecting",
    "completed",
  ];
  const PHASE_LABELS = {
    created: "Created",
    preflight: "Preflight",
    namespace_created: "Namespace",
    controller_ready: "Controller",
    pipeline_ready: "Pipeline",
    measuring: "Measuring",
    collecting: "Collecting",
    completed: "Completed",
    failed: "Failed",
    interrupted: "Interrupted",
    progress: "Progress",
  };

  const els = {
    tabs: [...document.querySelectorAll(".tab")],
    views: {
      reports: document.getElementById("view-reports"),
      run: document.getElementById("view-run"),
      validation: document.getElementById("view-validation"),
      history: document.getElementById("view-history"),
    },
    benchmark: document.getElementById("benchmark"),
    baseline: document.getElementById("baseline"),
    candidate: document.getElementById("candidate"),
    report: document.getElementById("report"),
    status: document.getElementById("status"),
    runScenario: document.getElementById("run-scenario"),
    runDuration: document.getElementById("run-duration"),
    runImage: document.getElementById("run-image"),
    runCli: document.getElementById("run-cli"),
    runForm: document.getElementById("run-form"),
    runLive: document.getElementById("run-live"),
    runDone: document.getElementById("run-done"),
    runStart: document.getElementById("run-start"),
    runPreflight: document.getElementById("run-preflight"),
    runCancel: document.getElementById("run-cancel"),
    runError: document.getElementById("run-error"),
    runBanner: document.getElementById("run-preflight-banner"),
    liveTitle: document.getElementById("live-title"),
    liveSubtitle: document.getElementById("live-subtitle"),
    liveStatus: document.getElementById("live-status"),
    liveNamespace: document.getElementById("live-namespace"),
    liveElapsed: document.getElementById("live-elapsed"),
    liveLog: document.getElementById("live-log"),
    phaseRail: document.getElementById("phase-rail"),
    doneTitle: document.getElementById("done-title"),
    doneSubtitle: document.getElementById("done-subtitle"),
    doneSummary: document.getElementById("done-summary"),
    doneViewReport: document.getElementById("done-view-report"),
    doneNewRun: document.getElementById("done-new-run"),
    historyScenario: document.getElementById("history-scenario"),
    historyStatus: document.getElementById("history-status"),
    historyRefresh: document.getElementById("history-refresh"),
    historyMsg: document.getElementById("history-status-msg"),
    historyBody: document.querySelector("#history-table tbody"),
    overwriteDialog: document.getElementById("overwrite-dialog"),
    overwriteMessage: document.getElementById("overwrite-message"),
  };

  let benchmarks = [];
  let config = {};
  let loadingReport = false;
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

  function setStatus(message, isError) {
    els.status.textContent = message || "";
    els.status.classList.toggle("error", Boolean(isError));
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

  function setView(name) {
    Object.entries(els.views).forEach(([key, node]) => {
      node.classList.toggle("hidden", key !== name);
    });
    els.tabs.forEach((tab) => {
      tab.classList.toggle("active", tab.dataset.view === name);
    });
    if (name === "history") refreshHistory();
  }

  function setRunControlsEnabled(enabled) {
    els.baseline.disabled = !enabled;
    els.candidate.disabled = !enabled;
  }

  function showNoReportsPage(scenario) {
    const command = `perfman benchmark run --scenario ${scenario} --image <numaflow-image>`;
    els.report.srcdoc = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;padding:2rem;font-family:system-ui,sans-serif;background:#0b0e11;color:#d8d9da}
.card{width:min(42rem,100%);padding:2rem;border:1px solid #2f3542;border-radius:8px;background:#181b1f;text-align:center}
h1{margin:0;color:#f5f6f7;font-size:1.6rem}
p{margin:1rem auto 0;max-width:34rem;color:#a9b0ba;line-height:1.55}
code{display:block;margin-top:1.25rem;padding:.85rem 1rem;overflow-x:auto;border:1px solid #2f3542;border-radius:8px;background:#111217;color:#f5f6f7;text-align:left}
.btn{display:inline-block;margin-top:1rem;padding:.5rem .9rem;border-radius:4px;background:#5794f2;color:#0b0e11;text-decoration:none;font-weight:600}
</style></head><body>
<section class="card">
  <h1>${escapeHTML(scenario)} has no completed reports</h1>
  <p>Start a benchmark from the Run tab, or use the CLI. After it completes, select it as a baseline.</p>
  <code>${escapeHTML(command)}</code>
</section></body></html>`;
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

  function populateSelect(select, options, selectedValue, includeEmpty, emptyLabel) {
    select.replaceChildren();
    if (includeEmpty) {
      const opt = document.createElement("option");
      opt.value = "";
      opt.textContent = emptyLabel || "— select —";
      select.appendChild(opt);
    }
    for (const item of options) {
      const opt = document.createElement("option");
      opt.value = item.id;
      opt.textContent = item.label;
      select.appendChild(opt);
    }
    if (selectedValue && options.some((o) => o.id === selectedValue)) {
      select.value = selectedValue;
    } else if (!includeEmpty && options.length > 0) {
      select.value = options[0].id;
    } else if (includeEmpty) {
      select.value = "";
    }
  }

  async function loadBenchmarks() {
    const data = await fetchJSON("/api/benchmarks");
    benchmarks = data.benchmarks || [];
    const fill = (select, includeAll) => {
      select.replaceChildren();
      if (includeAll) {
        const opt = document.createElement("option");
        opt.value = "";
        opt.textContent = "All scenarios";
        select.appendChild(opt);
      }
      for (const b of benchmarks) {
        const opt = document.createElement("option");
        opt.value = b.id;
        opt.textContent = b.description ? `${b.id} — ${b.description}` : b.id;
        select.appendChild(opt);
      }
    };
    fill(els.benchmark, false);
    fill(els.runScenario, false);
    fill(els.historyScenario, true);
    const def = data.default || benchmarks[0]?.id || "";
    els.benchmark.value = def;
    els.runScenario.value = def;
    if (config.image) els.runImage.value = config.image;
    updateCliPreview();
  }

  async function loadConfig() {
    try {
      config = await fetchJSON("/api/config");
    } catch (_) {
      config = { execution_enabled: false };
    }
    if (config.image && !els.runImage.value) els.runImage.value = config.image;
    if (!config.udf_image) {
      showBanner(els.runBanner, "UDF image is not configured. Run setup or: perfman config set --key udf_image --value <image>", "warn");
    } else if (!config.execution_enabled) {
      showBanner(els.runBanner, "Benchmark execution is not enabled on this server.", "warn");
    } else {
      showBanner(els.runBanner, "", null);
    }
  }

  function updateCliPreview() {
    const parts = [
      "perfman benchmark run",
      `--scenario ${els.runScenario.value || "<scenario>"}`,
      `--image ${els.runImage.value || "<image>"}`,
      `--duration ${els.runDuration.value || "15m"}`,
    ];
    els.runCli.textContent = parts.join(" ");
  }

  async function loadRuns() {
    const scenario = els.benchmark.value;
    if (!scenario) {
      populateSelect(els.baseline, [], "", false);
      populateSelect(els.candidate, [], "", true);
      setRunControlsEnabled(false);
      return false;
    }
    const data = await fetchJSON(`/api/runs?scenario=${encodeURIComponent(scenario)}`);
    const runs = data.runs || [];
    const prevBaseline = els.baseline.value;
    const prevCandidate = els.candidate.value;
    populateSelect(els.baseline, runs, prevBaseline, false);
    if (runs.length === 0) {
      populateSelect(els.candidate, [], "", true);
      setRunControlsEnabled(false);
      setStatus("No reports yet for this benchmark.", false);
      showNoReportsPage(scenario);
      return false;
    }
    setRunControlsEnabled(true);
    populateSelect(els.candidate, runs, prevCandidate, true);
    if (!els.baseline.value) els.baseline.value = runs[0].id;
    return true;
  }

  async function loadReport() {
    const scenario = els.benchmark.value;
    const baseline = els.baseline.value;
    const candidate = els.candidate.value;
    const isComparison = candidate && candidate !== baseline;
    if (!scenario || !baseline) {
      els.report.removeAttribute("srcdoc");
      setStatus("", false);
      return;
    }
    loadingReport = true;
    setStatus("Loading report…", false);
    try {
      const url = isComparison
        ? `/api/compare?scenario=${encodeURIComponent(scenario)}&baseline_run_id=${encodeURIComponent(baseline)}&candidate_run_id=${encodeURIComponent(candidate)}`
        : `/api/report?scenario=${encodeURIComponent(scenario)}&run_id=${encodeURIComponent(baseline)}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(await res.text() || res.statusText);
      els.report.srcdoc = await res.text();
      setStatus(isComparison ? "Comparison report" : "Single-run report", false);
    } catch (err) {
      els.report.removeAttribute("srcdoc");
      setStatus(err.message || "Failed to load report", true);
    } finally {
      loadingReport = false;
    }
  }

  async function refreshReports(preferRunID) {
    try {
      if (await loadRuns()) {
        if (preferRunID && [...els.baseline.options].some((o) => o.value === preferRunID)) {
          els.baseline.value = preferRunID;
          els.candidate.value = "";
        }
        await loadReport();
      }
    } catch (err) {
      setStatus(err.message || "Failed to load data", true);
    }
  }

  function stopLiveWatchers() {
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

  function showRunMode(mode) {
    els.runForm.classList.toggle("hidden", mode !== "form");
    els.runLive.classList.toggle("hidden", mode !== "live");
    els.runDone.classList.toggle("hidden", mode !== "done");
  }

  function renderPhaseRail(status) {
    const display = PHASES.filter((p) => p !== "created");
    const failed = status === "failed" || status === "interrupted";
    let activeIdx = display.indexOf(status);
    if (failed) {
      const last = activeRun?.lastPhase;
      activeIdx = Math.max(0, display.indexOf(last));
    }
    if (status === "completed") activeIdx = display.length - 1;
    els.phaseRail.replaceChildren();
    display.forEach((phase, idx) => {
      const li = document.createElement("li");
      li.textContent = PHASE_LABELS[phase] || phase;
      if (failed && idx === activeIdx) li.classList.add("failed", "active");
      else if (!failed && idx < activeIdx) li.classList.add("done");
      else if (!failed && idx === activeIdx) li.classList.add("active");
      else if (status === "completed") li.classList.add("done");
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

  function formatElapsed(ms) {
    const total = Math.max(0, Math.floor(ms / 1000));
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }

  async function refreshLiveStatus(runID) {
    const info = await fetchJSON(`/api/benchmark-runs/${encodeURIComponent(runID)}`);
    els.liveStatus.textContent = info.status || "—";
    els.liveNamespace.textContent = info.namespace || "—";
    els.liveSubtitle.textContent = `${info.scenario} · ${info.image_ref}`;
    if (info.status && info.status !== "progress") {
      if (!activeRun) activeRun = { id: runID };
      activeRun.lastPhase = info.status;
      renderPhaseRail(info.status);
    }
    if (["completed", "failed", "interrupted"].includes(info.status) && !info.active) {
      finishRun(info);
    }
    return info;
  }

  function finishRun(info) {
    stopLiveWatchers();
    showRunMode("done");
    const ok = info.status === "completed";
    els.doneTitle.textContent = ok ? "Benchmark completed" : `Benchmark ${info.status}`;
    els.doneSubtitle.textContent = `${info.scenario} · ${info.image_ref}`;
    showBanner(
      els.doneSummary,
      ok
        ? `Run ${info.id} finished successfully. View the report or start another run.`
        : (info.error_message || `Run ended with status ${info.status}.`),
      ok ? "ok" : "error",
    );
    activeRun = info;
    if (ok) {
      els.benchmark.value = info.scenario;
      refreshReports(info.id);
    }
  }

  function startLiveWatch(runID) {
    stopLiveWatchers();
    liveStartedAt = Date.now();
    els.liveLog.replaceChildren();
    elapsedTimer = setInterval(() => {
      els.liveElapsed.textContent = formatElapsed(Date.now() - liveStartedAt);
    }, 1000);
    els.liveElapsed.textContent = "00:00";

    const url = `/api/benchmark-runs/${encodeURIComponent(runID)}/events?after_id=0`;
    if (window.EventSource) {
      eventSource = new EventSource(url);
      eventSource.onmessage = (msg) => {
        try {
          const ev = JSON.parse(msg.data);
          appendLog(ev);
          if (["completed", "failed", "interrupted"].includes(ev.phase)) {
            refreshLiveStatus(runID).catch(() => {});
          }
        } catch (_) { /* ignore */ }
      };
      eventSource.onerror = () => {
        // Fall back to polling if SSE drops.
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

  async function startRun(overwrite) {
    showBanner(els.runError, "", null);
    const body = {
      scenario: els.runScenario.value,
      image: els.runImage.value.trim(),
      duration: els.runDuration.value.trim() || "15m",
      overwrite: Boolean(overwrite),
    };

    els.runStart.disabled = true;
    try {
      const info = await fetchJSON("/api/benchmark-runs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      activeRun = info;
      showRunMode("live");
      els.liveTitle.textContent = "Live run";
      renderPhaseRail(info.status || "created");
      startLiveWatch(info.id);
      if (info.mutable_tag_warning) {
        appendLog({
          timestamp: new Date().toISOString(),
          level: "info",
          phase: "progress",
          message: `warning: image uses a mutable tag; prefer digests for reliable comparisons`,
        });
      }
    } catch (err) {
      if (err.status === 409 && err.data?.code === "overwrite_required") {
        const existing = err.data.existing_run;
        els.overwriteMessage.textContent = existing
          ? `A result already exists for this scenario/image (run ${existing.id}, status ${existing.status}). Overwrite it?`
          : (err.data.message || "Overwrite required");
        const confirmed = await new Promise((resolve) => {
          const onClose = () => {
            els.overwriteDialog.removeEventListener("close", onClose);
            resolve(els.overwriteDialog.returnValue === "confirm");
          };
          els.overwriteDialog.addEventListener("close", onClose);
          els.overwriteDialog.showModal();
        });
        if (confirmed) {
          await startRun(true);
        }
        return;
      }
      showBanner(els.runError, err.data?.message || err.message || "Failed to start run", "error");
    } finally {
      els.runStart.disabled = false;
    }
  }

  async function runPreflight() {
    showBanner(els.runBanner, "Running preflight checks…", null);
    els.runPreflight.disabled = true;
    try {
      const data = await fetchJSON("/api/preflight", { method: "POST" });
      const blockers = (data.checks || []).filter((c) => c.blocker && c.status === "fail");
      if (data.passed) {
        showBanner(els.runBanner, `Preflight passed (${(data.checks || []).length} checks).`, "ok");
      } else {
        showBanner(els.runBanner, `Preflight failed: ${blockers[0]?.message || "see doctor output"}`, "error");
      }
    } catch (err) {
      showBanner(els.runBanner, err.message || "Preflight failed", "error");
    } finally {
      els.runPreflight.disabled = false;
    }
  }

  async function refreshHistory() {
    els.historyMsg.textContent = "Loading…";
    try {
      const params = new URLSearchParams();
      if (els.historyScenario.value) params.set("scenario", els.historyScenario.value);
      if (els.historyStatus.value) params.set("status", els.historyStatus.value);
      params.set("limit", "100");
      const data = await fetchJSON(`/api/benchmark-runs?${params}`);
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
        if (run.status === "completed") {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.className = "btn secondary";
          btn.textContent = "Open report";
          btn.addEventListener("click", () => {
            els.benchmark.value = run.scenario;
            setView("reports");
            refreshReports(run.id);
          });
          actions.appendChild(btn);
        } else if (run.active || !["completed", "failed", "interrupted"].includes(run.status)) {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.className = "btn secondary";
          btn.textContent = "Watch";
          btn.addEventListener("click", () => {
            activeRun = run;
            setView("run");
            showRunMode("live");
            renderPhaseRail(run.status || "created");
            startLiveWatch(run.id);
          });
          actions.appendChild(btn);
        } else if (run.error_message) {
          actions.textContent = run.error_message;
        }
        els.historyBody.appendChild(tr);
      }
      els.historyMsg.textContent = runs.length ? `${runs.length} run(s)` : "No runs found.";
    } catch (err) {
      els.historyMsg.textContent = err.message || "Failed to load history";
      els.historyMsg.classList.add("error");
    }
  }

  // Events
  els.tabs.forEach((tab) => tab.addEventListener("click", () => setView(tab.dataset.view)));
  els.benchmark.addEventListener("change", () => refreshReports());
  els.baseline.addEventListener("change", () => { if (!loadingReport) loadReport(); });
  els.candidate.addEventListener("change", () => { if (!loadingReport) loadReport(); });

  ["change", "input"].forEach((evt) => {
    els.runScenario.addEventListener(evt, updateCliPreview);
    els.runDuration.addEventListener(evt, updateCliPreview);
    els.runImage.addEventListener(evt, updateCliPreview);
  });
  els.runStart.addEventListener("click", () => startRun(false));
  els.runPreflight.addEventListener("click", runPreflight);
  els.runCancel.addEventListener("click", async () => {
    if (!activeRun?.id) return;
    try {
      await fetchJSON(`/api/benchmark-runs/${encodeURIComponent(activeRun.id)}/cancel`, { method: "POST" });
      appendLog({
        timestamp: new Date().toISOString(),
        level: "info",
        phase: "progress",
        message: "cancellation requested",
      });
    } catch (err) {
      showBanner(els.runError, err.message || "Cancel failed", "error");
    }
  });
  els.doneNewRun.addEventListener("click", () => {
    activeRun = null;
    showBanner(els.runError, "", null);
    showRunMode("form");
  });
  els.doneViewReport.addEventListener("click", () => {
    if (activeRun?.scenario) {
      els.benchmark.value = activeRun.scenario;
      setView("reports");
      refreshReports(activeRun.id);
    }
  });
  els.historyRefresh.addEventListener("click", refreshHistory);
  els.historyScenario.addEventListener("change", refreshHistory);
  els.historyStatus.addEventListener("change", refreshHistory);

  (async () => {
    try {
      await loadConfig();
      await loadBenchmarks();
      await refreshReports();
      showRunMode("form");
    } catch (err) {
      setStatus(err.message || "Failed to initialize", true);
    }
  })();
})();
