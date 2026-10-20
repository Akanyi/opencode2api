// Page: 概览 — readiness checklist, metrics, resources and token usage.
import * as store from "../store.js";
import { $, bindTabs, el, emptyRow, formatNumber, timeOnly } from "../ui.js";
import { t, onChange } from "../i18n.js";

const proxyView = { page: 1, size: 20, query: "", health: "all" };

function renderProxyTable() {
  const monitor = store.get("monitor");
  if (!monitor) return;
  const all = monitor.resources?.proxies || [];
  const todayAttempts = monitor.upstream?.today?.proxies;
  const query = proxyView.query.trim().toLowerCase();
  const filtered = all.filter((proxy) =>
    (!query || (proxy.address || "").toLowerCase().includes(query)) &&
    (proxyView.health === "all" || proxy.healthy === (proxyView.health === "healthy")),
  );
  const pages = Math.max(1, Math.ceil(filtered.length / proxyView.size));
  proxyView.page = Math.min(proxyView.page, pages);
  const start = (proxyView.page - 1) * proxyView.size;
  const visible = filtered.slice(start, start + proxyView.size);
  const healthy = all.filter((proxy) => proxy.healthy).length;
  const summary = $("#proxy-pool-summary");
  summary.replaceChildren(
    el("span", { class: "proxy-summary-chip", text: t(`全部 ${formatNumber(all.length)}`, "overview.proxy.total", formatNumber(all.length)) }),
    el("span", { class: "proxy-summary-chip ok", text: t(`正常 ${formatNumber(healthy)}`, "overview.proxy.healthyCount", formatNumber(healthy)) }),
    el("span", { class: `proxy-summary-chip ${all.length > healthy ? "bad" : ""}`, text: t(`不可用 ${formatNumber(all.length - healthy)}`, "overview.proxy.unhealthyCount", formatNumber(all.length - healthy)) }),
  );
  const host = $("#proxy-table");
  const scroller = $(".proxy-table-scroll");
  const scrollTop = scroller.scrollTop;
  host.replaceChildren();
  visible.forEach((proxy) => {
    const attempts = todayAttempts?.[proxy.id] || {};
    host.append(el("tr", {}, [
      el("td", { class: "proxy-address" }, [el("span", { text: proxy.address, title: proxy.address })]),
      el("td", {}, [el("span", {
        class: `status ${proxy.healthy ? "ok" : "bad"}`,
        text: proxy.healthy ? t("正常", "state.healthy") : t("不可用", "state.unavailable"),
      })]),
      el("td", { text: `Z ${proxy.zen_keys} · G ${proxy.go_keys} · A ${proxy.anonymous ? t("开", "common.on") : t("关", "common.off")}` }),
      el("td", { class: "num", text: formatNumber(proxy.active_connections || 0) }),
      el("td", { class: "num", text: formatNumber(proxy.sticky_sessions || 0) }),
      el("td", { class: "num", text: `${formatNumber(attempts.total)} / ${formatNumber(attempts.success)} / ${formatNumber(attempts.failed)}` }),
    ]));
  });
  if (!visible.length) emptyRow(host, 6, all.length ? t("没有匹配的代理节点", "overview.proxy.noMatch") : t("暂无代理节点", "overview.proxy.empty"));
  scroller.scrollTop = scrollTop;
  const info = $("#proxy-page-info");
  const text = t(`${filtered.length ? start + 1 : 0}–${start + visible.length} / ${formatNumber(filtered.length)} 条 · ${proxyView.page} / ${pages} 页`,
    "overview.proxy.pageInfo", filtered.length ? start + 1 : 0, start + visible.length, formatNumber(filtered.length), proxyView.page, pages);
  // Avoid repeated live-region announcements during the automatic refresh.
  if (info.textContent !== text) info.textContent = text;
  $("#proxy-prev").disabled = proxyView.page <= 1;
  $("#proxy-next").disabled = proxyView.page >= pages;
}

function updateProxyView(change) {
  Object.assign(proxyView, change);
  renderProxyTable();
  $(".proxy-table-scroll").scrollTop = 0;
}

function renderChecklist() {
  const config = store.get("config");
  const monitor = store.get("monitor");
  if (!config || !monitor) return;
  const resources = monitor.resources;
  const models = resources.models || {};
  const modelDescription = models.stale
    ? t(
        `目录来自${models.cache_source || "磁盘"}快取，等待后台刷新。`,
        "guide.models.cached",
        models.cache_source || "磁盘",
      )
    : t("上游目录已完成首次刷新且有可暴露模型。", "guide.models.ready");
  const checks = [
    [
      t("配置本地 Key", "guide.step.serverkey"),
      t("客户端使用 Server Key 访问 Gateway。", "guide.step.serverkey.desc"),
      (config.server_keys || []).length > 0,
    ],
    [
      t("选择上游通道", "guide.step.upstream"),
      t("至少有 Zen / Go Key，或启用匿名通道。", "guide.step.upstream.desc"),
      (config.zen_keys || []).length + (config.go_keys || []).length > 0 || config.anonymous,
    ],
    [
      t("检查 Proxy", "guide.step.proxy"),
      t("至少一个代理节点健康。", "guide.step.proxy.desc"),
      (resources.proxies || []).some((proxy) => proxy.healthy),
    ],
    [
      t("等待模型目录", "guide.step.models"),
      modelDescription,
      models.exposed > 0 && !!models.updated_at,
    ],
  ];
  const card = $("#overview-checklist");
  const ready = checks.every(([, , done]) => done);
  card.classList.toggle("hidden", ready);
  if (ready) return;
  const host = $("#guide-steps");
  host.replaceChildren();
  checks.forEach(([title, description, done]) => {
    host.append(
      el("article", { class: "guide-step" }, [
        el("h3", { text: title }),
        el("p", { text: description }),
        el("div", {
          class: `step-state ${done ? "ok" : "warn"}`,
          text: done ? t("✓ 已就绪", "guide.done") : t("○ 待处理", "guide.todo"),
        }),
      ]),
    );
  });
}

function renderResources(resources) {
  const strategies = { affinity: "会话固定", sticky_round_robin: "会话固定轮询", round_robin: "轮询", random: "随机", least_connections: "最少连接" };
  $("#proxy-strategy-summary").textContent = `当前调度：${strategies[resources.proxy_strategy] || "会话固定"} · 连接数包含流式响应与后台检查`;
  if (resources.proxy_strategy === "sticky_round_robin") {
    $("#proxy-strategy-summary").textContent += ` · 空闲 ${resources.session_idle_ttl_seconds / 60} 分钟释放 · 会话迁移 ${resources.session_migrations || 0} 次（进程累计）`;
  }
  renderProxyTable();
  const keys = $("#key-table");
  keys.replaceChildren();
  if (resources.anonymous) {
    keys.append(
      el("tr", {}, [
        el("td", { text: "anonymous" }),
        el("td", { text: "zen" }),
        el("td", {}, [el("span", { class: "status ok", text: t("已启用", "state.enabled") })]),
      ]),
    );
  }
  (resources.keys || []).forEach((key) => {
    keys.append(
      el("tr", {}, [
        el("td", { text: key.id }),
        el("td", { text: key.tier }),
        el("td", {}, [el("span", {
          class: `status ${key.cooldown_until ? "warn" : "ok"}`,
          text: key.cooldown_until ? t("冷却中", "state.cooling") : t("可用", "state.available"),
        })]),
      ]),
    );
  });
  if (!keys.children.length) emptyRow(keys, 3);
}

function renderBreakdowns(metrics) {
  const host = $("#breakdowns");
  host.replaceChildren();
  [
    [t("端点", "breakdown.endpoint"), metrics.endpoints],
    [t("状态码", "breakdown.status"), metrics.statuses],
    [t("模型", "breakdown.model"), metrics.models],
    ["Tier", metrics.tiers],
  ].forEach(([title, data]) => {
    const body = el("tbody");
    Object.entries(data || {})
      .sort((a, b) => b[1] - a[1])
      .slice(0, 10)
      .forEach(([key, value]) => {
        body.append(el("tr", {}, [el("td", { text: key }), el("td", { class: "num", text: value })]));
      });
    if (!body.children.length) emptyRow(body, 2, t("暂无请求", "breakdown.empty"));
    host.append(
      el("div", { class: "breakdown" }, [
        el("h3", { text: title }),
        el("div", { class: "table-wrap" }, [el("table", { class: "table" }, [body])]),
      ]),
    );
  });
}

function renderSummary(monitor) {
  const metrics = monitor.metrics;
  const today = metrics.today || {};
  $("#m-total").textContent = formatNumber(today.total);
  $("#m-rate").textContent = `${((today.success_rate || 0) * 100).toFixed(1)}%`;
  $("#m-p95").textContent = `${today.p95_ms || 0} ms`;
  $("#m-active").textContent = `${metrics.active_requests || 0} / ${metrics.active_streams || 0}`;
  $("#m-peak").textContent = `今日峰值并发 ${today.peak_concurrency || 0}`;
  const since = new Date(Math.max(Date.parse(metrics.day_start), Date.parse(metrics.started_at)));
  $("#statistics-period").textContent = `今日 · ${metrics.timezone || "Asia/Singapore"}`;
  $("#statistics-period").title = `采集开始：${since.toLocaleString(undefined, { timeZone: metrics.timezone || "Asia/Singapore" })}；进程重启会清空内存统计`;
  renderResources(monitor.resources);
  renderBreakdowns(metrics);
}

function renderTrend(metrics) {
  const trend = $("#usage-trend");
  const groups = new Map();
  for (const point of metrics?.today_series || []) {
    const minute = Math.floor(Date.parse(point.minute) / 900000) * 900000;
    const group = groups.get(minute) || { minute: new Date(minute).toISOString(), input_tokens: 0, output_tokens: 0, total_tokens: 0 };
    for (const key of ["input_tokens", "output_tokens", "total_tokens"]) group[key] += point[key] || 0;
    groups.set(minute, group);
  }
  const series = [...groups.values()];
  const maximum = Math.max(1, ...series.map((point) => point.total_tokens || 0));
  trend.replaceChildren();
  series.forEach((point) => {
    const bar = el("div", { class: "trend-bar" });
    bar.style.height = `${Math.max(2, Math.round(((point.total_tokens || 0) / maximum) * 100))}%`;
    bar.title = `${timeOnly(point.minute)} · input ${formatNumber(point.input_tokens)} · output ${formatNumber(point.output_tokens)} · total ${formatNumber(point.total_tokens)}`;
    trend.append(bar);
  });
  $("#usage-trend-meta").textContent = series.length
    ? `今日峰值 ${formatNumber(maximum)} tokens / 15 分钟 · ${metrics.timezone || "Asia/Singapore"}`
    : t("尚无趋势数据", "usage.trend.empty");
}

function renderUsageModels(usage) {
  const host = $("#usage-models");
  host.replaceChildren();
  Object.entries(usage.models || {})
    .sort((a, b) => (b[1].total_tokens || 0) - (a[1].total_tokens || 0))
    .slice(0, 15)
    .forEach(([model, value]) => {
      host.append(
        el("tr", {}, [
          el("td", { class: "mono", text: model }),
          el("td", { class: "num", text: formatNumber(value.input_tokens) }),
          el("td", { class: "num", text: formatNumber(value.output_tokens) }),
          el("td", { class: "num", text: formatNumber(value.total_tokens) }),
        ]),
      );
    });
  if (!host.children.length) emptyRow(host, 4, t("尚无 Token 用量", "usage.empty.models"));
}

function renderTiers(usage) {
  const host = $("#usage-tiers");
  const entries = Object.entries(usage.tiers || {}).sort(
    (a, b) => (b[1].total_tokens || 0) - (a[1].total_tokens || 0),
  );
  const total = Math.max(1, ...entries.map(([, value]) => value.total_tokens || 0));
  host.replaceChildren();
  entries.forEach(([tier, value]) => {
    const fill = el("div", { class: "tier-fill" });
    fill.style.width = `${Math.round(((value.total_tokens || 0) / total) * 100)}%`;
    host.append(
      el("div", { class: "tier-row" }, [
        el("strong", { text: tier }),
        el("div", { class: "tier-track" }, [fill]),
        el("span", { class: "tier-value", text: formatNumber(value.total_tokens) }),
      ]),
    );
  });
  if (!entries.length) host.append(el("div", { class: "empty", text: t("尚无 Tier 用量", "usage.empty.tiers") }));
}

function renderUsage(monitor) {
  const usage = monitor.usage?.today || {};
  const tokens = usage.tokens || {};
  $("#u-coverage").textContent = `${((usage.coverage || 0) * 100).toFixed(1)}%`;
  $("#u-total").textContent = formatNumber(tokens.total_tokens);
  $("#u-cached").textContent = formatNumber(tokens.cached_tokens);
  $("#u-reasoning").textContent = formatNumber(tokens.reasoning_tokens);
  renderTrend(monitor.metrics);
  renderUsageModels(usage);
  renderTiers(usage);
}

function render() {
  renderChecklist();
  const monitor = store.get("monitor");
  if (!monitor) return;
  renderSummary(monitor);
  renderUsage(monitor);
}

export default {
  id: "overview",
  title: "概览",
  sub: "运行指标、Token 用量与资源状态。",
  init() {
    bindTabs($("#page-overview"));
    $("#proxy-search").addEventListener("input", (event) => updateProxyView({ query: event.target.value, page: 1 }));
    $("#proxy-health-filter").addEventListener("change", (event) => updateProxyView({ health: event.target.value, page: 1 }));
    $("#proxy-page-size").addEventListener("change", (event) => updateProxyView({ size: Number(event.target.value), page: 1 }));
    $("#proxy-prev").addEventListener("click", () => updateProxyView({ page: Math.max(1, proxyView.page - 1) }));
    $("#proxy-next").addEventListener("click", () => updateProxyView({ page: proxyView.page + 1 }));
    store.on("monitor", render);
    store.on("config", renderChecklist);
    onChange(render);
  },
  show: render,
};
