// 첫 화면을 그리기 전에 저장된 테마(밝게/어둡게)를 적용한다. 저장값이 없으면 시스템 설정을 따른다.
(function () {
  try {
    var t = localStorage.getItem("mh-theme");
    if (t === "light" || t === "dark") document.documentElement.setAttribute("data-theme", t);
  } catch (e) { /* 저장소를 못 쓰면 시스템 설정 */ }
})();
