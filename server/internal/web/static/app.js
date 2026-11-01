// 공통 동작: 테마 전환, 알림(toast), htmx 오류 표시, 확인 대화상자, 금액 입력 쉼표.
(function () {
  "use strict";

  function toast(msg, isErr) {
    var el = document.getElementById("toast");
    if (!el) return;
    el.textContent = msg;
    el.className = "toast" + (isErr ? " err" : "");
    el.hidden = false;
    clearTimeout(el._t);
    el._t = setTimeout(function () { el.hidden = true; }, 3500);
  }

  // 테마 전환: 시스템 → 반대 → 저장
  var btn = document.getElementById("theme-toggle");
  if (btn) {
    btn.addEventListener("click", function () {
      var root = document.documentElement;
      var cur = root.getAttribute("data-theme");
      if (!cur) cur = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
      var next = cur === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("mh-theme", next); } catch (e) {}
      document.dispatchEvent(new CustomEvent("mh:theme"));
    });
  }

  // 서버가 HX-Trigger: {"toast": "..."}로 보낸 안내
  document.body.addEventListener("toast", function (e) {
    var v = e.detail && (e.detail.value || e.detail);
    if (typeof v === "string") toast(v);
  });

  // htmx 요청 실패를 사용자에게 보여준다(400 등은 swap하지 않음)
  document.body.addEventListener("htmx:responseError", function (e) {
    var text = (e.detail.xhr && e.detail.xhr.responseText) || "요청 실패";
    toast(text.slice(0, 200), true);
  });
  document.body.addEventListener("htmx:sendError", function () { toast("서버에 연결하지 못했어요", true); });

  // data-confirm이 붙은 일반 폼 제출 전 확인
  document.addEventListener("submit", function (e) {
    var f = e.target;
    if (f && f.dataset && f.dataset.confirm && !window.confirm(f.dataset.confirm)) e.preventDefault();
  });

  // 금액 입력란: 숫자만 남기고 쉼표 표시(서버는 쉼표를 지우고 읽는다)
  document.addEventListener("input", function (e) {
    var el = e.target;
    if (!el.classList || !el.classList.contains("money")) return;
    var digits = el.value.replace(/[^0-9]/g, "");
    el.value = digits ? Number(digits).toLocaleString("ko-KR") : "";
  });
})();
