// 분석 화면 차트. 데이터는 페이지의 <script type="application/json" id="analytics-data">에서 읽는다.
(function () {
  "use strict";
  var el = document.getElementById("analytics-data");
  if (!el || typeof Chart === "undefined") return;
  var data = JSON.parse(el.textContent);
  var charts = [];

  function css(name) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }
  function won(v) { return Number(v).toLocaleString("ko-KR") + "원"; }
  function monthName(label) { return Number(label.slice(5)) + "월"; }
  // 분류 색: 테마 토큰에 정의한 팔레트를 순서대로 쓴다
  function palette(n) {
    var out = [];
    for (var i = 0; i < n; i++) out.push(css("--c" + ((i % 10) + 1)));
    return out;
  }

  function draw() {
    charts.forEach(function (c) { c.destroy(); });
    charts = [];
    Chart.defaults.color = css("--muted");
    Chart.defaults.borderColor = css("--line");
    Chart.defaults.plugins.legend.labels.boxWidth = 10;
    Chart.defaults.font.family = css("--font");

    // 폴더 값은 안의 분류를 더한 것이라 맨 위 분류(depth 0)만 써야 두 번 세지 않는다
    var top = (data.categories || []).filter(function (c) { return !c.depth; });
    var cats = top.filter(function (c) { return c.current > 0; })
      .sort(function (a, b) { return b.current - a.current; });

    charts.push(new Chart(document.getElementById("chart-share"), {
      type: "doughnut",
      data: { labels: cats.map(function (c) { return c.name; }), datasets: [{ data: cats.map(function (c) { return c.current; }), backgroundColor: palette(cats.length), borderColor: css("--card-solid"), borderWidth: 3, borderRadius: 4 }] },
      options: { maintainAspectRatio: false, cutout: "64%", plugins: { legend: { position: "right", labels: { usePointStyle: true, pointStyle: "circle" } }, tooltip: { callbacks: { label: function (ctx) {
        var total = ctx.dataset.data.reduce(function (a, b) { return a + b; }, 0);
        return ctx.label + ": " + won(ctx.raw) + " (" + Math.round(ctx.raw * 100 / total) + "%)";
      } } } } }
    }));

    var cur = data.daily || [], prev = data.previous_daily || [];
    charts.push(new Chart(document.getElementById("chart-daily"), {
      type: "line",
      data: {
        labels: cur.map(function (d) { return d.day.slice(5).replace("-", "/"); }),
        datasets: [
          { label: monthName(data.period.label), data: cur.map(function (d) { return d.day > data.today ? null : d.spent; }), borderColor: css("--accent"), backgroundColor: css("--accent"), tension: 0.25, pointRadius: 2 },
          { label: monthName(data.previous_period.label), data: prev.map(function (d) { return d.spent; }), borderColor: css("--muted"), borderDash: [4, 4], pointRadius: 0, tension: 0.25 }
        ]
      },
      options: { maintainAspectRatio: false, interaction: { mode: "index", intersect: false },
        scales: { y: { ticks: { callback: function (v) { return (v / 10000) + "만"; } } } },
        plugins: { tooltip: { callbacks: { label: function (ctx) { return ctx.dataset.label + ": " + won(ctx.raw); } } } } }
    }));

    var both = top.filter(function (c) { return c.current || c.previous; });
    charts.push(new Chart(document.getElementById("chart-compare"), {
      type: "bar",
      data: {
        labels: both.map(function (c) { return c.name; }),
        datasets: [
          { label: monthName(data.previous_period.label), data: both.map(function (c) { return c.previous; }), backgroundColor: css("--muted-bar"), borderRadius: 6 },
          { label: monthName(data.period.label), data: both.map(function (c) { return c.current; }), backgroundColor: css("--accent"), borderRadius: 6 }
        ]
      },
      options: { maintainAspectRatio: false, scales: { y: { ticks: { callback: function (v) { return (v / 10000) + "만"; } } } },
        plugins: { tooltip: { callbacks: { label: function (ctx) { return ctx.dataset.label + ": " + won(ctx.raw); } } } } }
    }));
  }

  draw();
  document.addEventListener("mh:theme", draw);
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", draw);
})();
