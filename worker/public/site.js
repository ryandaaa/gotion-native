// The website's two behaviors: the language switch, and the download
// button that offers the build for the visitor's system.
(function () {
  var root = document.documentElement;

  function setLang(l) {
    root.dataset.lang = l;
    root.lang = l;
    var t = root.getAttribute("data-title-" + l);
    if (t) document.title = t;
    try { localStorage.setItem("lang", l); } catch (e) {}
  }
  setLang(root.dataset.lang === "id" ? "id" : "en");
  document.querySelectorAll("[data-lang-switch]").forEach(function (b) {
    b.addEventListener("click", function () {
      setLang(root.dataset.lang === "id" ? "en" : "id");
    });
  });

  // The visitor's system, as far as the browser tells.
  function system() {
    var ua = navigator.userAgent || "";
    var p = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || "";
    if (/Win/i.test(p) || /Windows/i.test(ua)) return "windows";
    if (/Mac/i.test(p) || /Macintosh/i.test(ua)) return /iPhone|iPad/i.test(ua) ? "" : "mac";
    if (/Android|iPhone|iPad/i.test(ua)) return "";
    if (/Linux/i.test(p) || /Linux/i.test(ua)) return "linux";
    return "";
  }
  var labels = {
    windows: { href: "/download/windows", en: "Download for Windows", id: "Unduh untuk Windows" },
    mac: { href: "/download/mac", en: "Download for Mac", id: "Unduh untuk Mac" },
    linux: { href: "/download/linux-deb", en: "Download for Linux (.deb)", id: "Unduh untuk Linux (.deb)" },
  };
  var os = system();
  var pick = labels[os];
  // On a phone, or a system not told, the button leads to every download.
  if (pick) {
    document.querySelectorAll("[data-download]").forEach(function (a) {
      a.href = pick.href;
      a.querySelector('[lang="en"]').textContent = pick.en;
      a.querySelector('[lang="id"]').textContent = pick.id;
    });
    var row = document.getElementById("dl-" + os);
    if (row) row.classList.add("mine");
  }

  // Copying the install command.
  document.querySelectorAll("[data-copy]").forEach(function (b) {
    b.addEventListener("click", function () {
      var text = document.getElementById(b.getAttribute("data-copy")).textContent;
      navigator.clipboard.writeText(text).then(function () {
        b.querySelectorAll("[data-done]").forEach(function (s) { s.hidden = false; });
        b.querySelectorAll("[data-idle]").forEach(function (s) { s.hidden = true; });
        setTimeout(function () {
          b.querySelectorAll("[data-done]").forEach(function (s) { s.hidden = true; });
          b.querySelectorAll("[data-idle]").forEach(function (s) { s.hidden = false; });
        }, 1600);
      });
    });
  });
})();
