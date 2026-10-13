package http

import (
	"encoding/json"
	"net/http"
)

func (s *Server) setupDashboardRoutes() {
	s.router.Get("/dashboard", s.handleDashboardHTML)
	s.router.Get("/api/queue", s.handleAPIQueue)
}

func (s *Server) handleAPIQueue(w http.ResponseWriter, r *http.Request) {
	jobs := s.queueManager.GetAllJobs()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jobs":  jobs,
		"total": len(jobs),
	})
}

func (s *Server) handleDashboardHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="vi" class="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Media Stream Bridge - Dashboard</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <style>
    body { background-color: #0b0f19; color: #e2e8f0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    .glass { background: rgba(30, 41, 59, 0.7); backdrop-filter: blur(12px); border: 1px solid rgba(255, 255, 255, 0.08); }
    .progress-bar { transition: width 0.4s ease-in-out; }
  </style>
</head>
<body class="p-6 max-w-6xl mx-auto">
  <!-- Header -->
  <header class="flex items-center justify-between pb-6 mb-8 border-b border-slate-800">
    <div class="flex items-center space-x-3">
      <div class="w-10 h-10 rounded-xl bg-gradient-to-tr from-cyan-500 to-blue-600 flex items-center justify-center shadow-lg shadow-cyan-500/20">
        <svg class="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"></path></svg>
      </div>
      <div>
        <h1 class="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-cyan-400 to-blue-400">Media Stream Bridge</h1>
        <p class="text-xs text-slate-400">Torznab Indexer & Download Queue Manager for *arr</p>
      </div>
    </div>
    <div class="flex items-center space-x-3">
      <span class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-950 text-emerald-400 border border-emerald-800">
        <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 mr-1.5 animate-pulse"></span> Service Active (Port 6890)
      </span>
    </div>
  </header>

  <!-- Stats Grid -->
  <div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-8">
    <div class="glass rounded-xl p-4">
      <div class="text-xs text-slate-400 uppercase tracking-wider font-semibold">Active Downloads</div>
      <div class="text-2xl font-bold text-cyan-400 mt-1" id="stat-active">0</div>
    </div>
    <div class="glass rounded-xl p-4">
      <div class="text-xs text-slate-400 uppercase tracking-wider font-semibold">In Queue</div>
      <div class="text-2xl font-bold text-amber-400 mt-1" id="stat-queued">0</div>
    </div>
    <div class="glass rounded-xl p-4">
      <div class="text-xs text-slate-400 uppercase tracking-wider font-semibold">Completed</div>
      <div class="text-2xl font-bold text-emerald-400 mt-1" id="stat-completed">0</div>
    </div>
    <div class="glass rounded-xl p-4">
      <div class="text-xs text-slate-400 uppercase tracking-wider font-semibold">Total Download Speed</div>
      <div class="text-2xl font-bold text-indigo-400 mt-1" id="stat-speed">0 MB/s</div>
    </div>
  </div>

  <!-- Queue Table -->
  <div class="glass rounded-2xl overflow-hidden shadow-2xl">
    <div class="p-4 border-b border-slate-800/80 flex items-center justify-between">
      <h2 class="font-semibold text-slate-200 text-sm flex items-center">
        <svg class="w-4 h-4 mr-2 text-cyan-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16m-7 6h7"></path></svg>
        Download Queue
      </h2>
      <button onclick="fetchQueue()" class="text-xs text-cyan-400 hover:text-cyan-300 transition flex items-center">
        <svg class="w-3.5 h-3.5 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path></svg>
        Refresh
      </button>
    </div>
    <div class="overflow-x-auto">
      <table class="w-full text-left text-sm text-slate-300">
        <thead class="bg-slate-900/40 text-xs uppercase text-slate-400 border-b border-slate-800">
          <tr>
            <th class="p-3 pl-4">Title / Media</th>
            <th class="p-3">Category</th>
            <th class="p-3">Status</th>
            <th class="p-3 w-48">Progress</th>
            <th class="p-3">Speed / ETA</th>
            <th class="p-3 text-right pr-4">Actions</th>
          </tr>
        </thead>
        <tbody id="queue-body" class="divide-y divide-slate-800/50">
          <tr>
            <td colspan="6" class="p-8 text-center text-slate-500">No active jobs in queue. Waiting for Sonarr/Radarr...</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <script>
    function formatBytes(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const k = 1024, dm = 2, sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
    }

    function formatTime(secs) {
      if (!secs || secs <= 0) return '--';
      const m = Math.floor(secs / 60), s = secs % 60;
      if (m > 60) return Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
      return m + 'm ' + s + 's';
    }

    async function fetchQueue() {
      try {
        const res = await fetch('/api/queue');
        const data = await res.json();
        renderQueue(data.jobs || []);
      } catch (e) {
        console.error(e);
      }
    }

    function renderQueue(jobs) {
      let active = 0, queued = 0, completed = 0, totalSpeed = 0;
      const tbody = document.getElementById('queue-body');

      if (jobs.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="p-8 text-center text-slate-500">No active jobs in queue. Waiting for Sonarr/Radarr...</td></tr>';
        updateStats(0, 0, 0, 0);
        return;
      }

      let html = '';
      jobs.forEach(j => {
        if (j.state === 'downloading') { active++; totalSpeed += (j.speed_bytes || 0); }
        else if (j.state === 'queued') queued++;
        else if (j.state === 'uploading') completed++;

        const pct = Math.round((j.progress || 0) * 100);
        let badge = '<span class="px-2 py-0.5 rounded text-xs bg-slate-800 text-slate-400">Queued</span>';
        if (j.state === 'downloading') badge = '<span class="px-2 py-0.5 rounded text-xs bg-cyan-950 text-cyan-400 border border-cyan-800">Downloading</span>';
        else if (j.state === 'uploading') badge = '<span class="px-2 py-0.5 rounded text-xs bg-emerald-950 text-emerald-400 border border-emerald-800">Completed</span>';
        else if (j.state === 'error') badge = '<span class="px-2 py-0.5 rounded text-xs bg-rose-950 text-rose-400 border border-rose-800">Error</span>';
        else if (j.state === 'paused') badge = '<span class="px-2 py-0.5 rounded text-xs bg-amber-950 text-amber-400 border border-amber-800">Paused</span>';

        html += '<tr class="hover:bg-slate-800/20 transition">';
        html += '<td class="p-3 pl-4 font-medium text-slate-200">' + j.title + '<div class="text-xs text-slate-500 font-mono mt-0.5">' + (j.save_path || '') + '</div></td>';
        html += '<td class="p-3 text-xs uppercase text-slate-400 font-semibold">' + (j.category || 'tv') + '</td>';
        html += '<td class="p-3">' + badge + '</td>';
        html += '<td class="p-3"><div class="w-full bg-slate-800 rounded-full h-2 overflow-hidden"><div class="bg-cyan-500 h-2 rounded-full progress-bar" style="width: ' + pct + '%"></div></div><div class="text-xs text-slate-400 mt-1">' + pct + '% (' + formatBytes(j.done_bytes) + ' / ' + formatBytes(j.total_bytes) + ')</div></td>';
        html += '<td class="p-3 text-xs text-slate-400">' + (j.state === 'downloading' ? formatBytes(j.speed_bytes) + '/s • ETA ' + formatTime(j.eta) : '--') + '</td>';
        html += '<td class="p-3 text-right pr-4 space-x-1">';
        if (j.state === 'downloading') {
          html += '<button onclick="pauseJob(\'' + j.hash + '\')" class="p-1 text-slate-400 hover:text-amber-400 transition" title="Pause"><svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg></button>';
        } else if (j.state === 'paused') {
          html += '<button onclick="resumeJob(\'' + j.hash + '\')" class="p-1 text-slate-400 hover:text-cyan-400 transition" title="Resume"><svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg></button>';
        }
        html += '<button onclick="deleteJob(\'' + j.hash + '\')" class="p-1 text-slate-400 hover:text-rose-400 transition" title="Delete"><svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg></button>';
        html += '</td></tr>';
      });

      tbody.innerHTML = html;
      updateStats(active, queued, completed, totalSpeed);
    }

    function updateStats(active, queued, completed, speed) {
      document.getElementById('stat-active').innerText = active;
      document.getElementById('stat-queued').innerText = queued;
      document.getElementById('stat-completed').innerText = completed;
      document.getElementById('stat-speed').innerText = (speed / (1024 * 1024)).toFixed(1) + ' MB/s';
    }

    async function pauseJob(hash) {
      const form = new FormData(); form.append('hashes', hash);
      await fetch('/api/v2/torrents/pause', { method: 'POST', body: form });
      fetchQueue();
    }

    async function resumeJob(hash) {
      const form = new FormData(); form.append('hashes', hash);
      await fetch('/api/v2/torrents/resume', { method: 'POST', body: form });
      fetchQueue();
    }

    async function deleteJob(hash) {
      if (!confirm('Cancel and delete this download job?')) return;
      const form = new FormData(); form.append('hashes', hash); form.append('deleteFiles', 'false');
      await fetch('/api/v2/torrents/delete', { method: 'POST', body: form });
      fetchQueue();
    }

    fetchQueue();
    setInterval(fetchQueue, 2000);
  </script>
</body>
</html>`
