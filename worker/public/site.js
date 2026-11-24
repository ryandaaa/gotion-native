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

  // Light or dark: the system's until the switch is used, then the one
  // chosen, kept for the next visit. The screenshot follows it.
  var sysDark = window.matchMedia("(prefers-color-scheme: dark)");
  function isDark() {
    var t = root.dataset.theme;
    return t ? t === "dark" : sysDark.matches;
  }
  function showTheme() {
    var d = isDark();
    document.querySelectorAll("picture source[data-dark]").forEach(function (src) {
      src.media = d ? "all" : "not all";
    });
    document.dispatchEvent(new Event("themechange"));
  }
  showTheme();
  if (sysDark.addEventListener) sysDark.addEventListener("change", showTheme);
  document.querySelectorAll("[data-theme-switch]").forEach(function (b) {
    b.addEventListener("click", function () {
      var t = isDark() ? "light" : "dark";
      root.dataset.theme = t;
      try { localStorage.setItem("theme", t); } catch (e) {}
      showTheme();
    });
  });

  // The scroll progress line, where CSS can't move it by itself.
  var bar = document.createElement("div");
  bar.className = "progress";
  bar.setAttribute("aria-hidden", "true");
  document.body.prepend(bar);
  if (!(window.CSS && CSS.supports && CSS.supports("animation-timeline: scroll()"))) {
    var ticking = false;
    var paint = function () {
      ticking = false;
      var max = document.documentElement.scrollHeight - innerHeight;
      bar.style.setProperty("--p", max > 0 ? Math.min(scrollY / max, 1) : 0);
    };
    addEventListener("scroll", function () { if (!ticking) { ticking = true; requestAnimationFrame(paint); } }, { passive: true });
    addEventListener("resize", paint);
    paint();
  }

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

// The dot grid behind the first screen: a vanilla port of React Bits' Dot
// Grid. Dots near a moving cursor are flicked along with it and spring
// back like jelly, take the accent color as the cursor nears, and a click
// sends a shockwave out. It draws only while something moves, rests when
// scrolled away, and stays still for those who ask for less motion.
(function () {
  var canvas = document.querySelector("[data-dots]");
  if (!canvas || !canvas.getContext) return;
  var ctx = canvas.getContext("2d");
  var GAP = 26, R = 1.6, NEAR = 150, WAVE = 260;
  var still = window.matchMedia("(prefers-reduced-motion: reduce)");
  var dots = [], w = 0, h = 0, dpr = 1, base = "", accent = "";
  var px = -1e4, py = -1e4, pvx = 0, pvy = 0, lastT = 0;
  var running = false, visible = true;

  function colors() {
    var cs = getComputedStyle(document.documentElement);
    base = cs.getPropertyValue("--dot").trim() || "rgba(0,0,0,.16)";
    accent = cs.getPropertyValue("--accent").trim() || "#7c5cd6";
  }

  function layout() {
    var r = canvas.getBoundingClientRect();
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    w = r.width; h = r.height;
    canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
    var cols = Math.floor(w / GAP), rows = Math.floor(h / GAP);
    var x0 = (w - (cols - 1) * GAP) / 2, y0 = (h - (rows - 1) * GAP) / 2;
    dots = [];
    for (var j = 0; j < rows; j++)
      for (var i = 0; i < cols; i++)
        dots.push({ hx: x0 + i * GAP, hy: y0 + j * GAP, ox: 0, oy: 0, vx: 0, vy: 0, a: 0 });
    draw();
  }

  function draw() {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = base;
    ctx.beginPath();
    for (var k = 0; k < dots.length; k++) {
      var d = dots[k];
      ctx.moveTo(d.hx + d.ox + R, d.hy + d.oy);
      ctx.arc(d.hx + d.ox, d.hy + d.oy, R, 0, 6.2832);
    }
    ctx.fill();
    ctx.fillStyle = accent;
    for (k = 0; k < dots.length; k++) {
      d = dots[k];
      if (d.a < 0.02) continue;
      ctx.globalAlpha = d.a;
      ctx.beginPath();
      ctx.arc(d.hx + d.ox, d.hy + d.oy, R + 0.6 * d.a, 0, 6.2832);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
  }

  // One step: springs pull every dot home; the cursor colors those near it.
  function step() {
    var moving = false;
    for (var k = 0; k < dots.length; k++) {
      var d = dots[k];
      d.vx = (d.vx - d.ox * 0.06) * 0.88;
      d.vy = (d.vy - d.oy * 0.06) * 0.88;
      d.ox += d.vx; d.oy += d.vy;
      var dx = d.hx - px, dy = d.hy - py, dist = Math.sqrt(dx * dx + dy * dy);
      var target = dist < NEAR ? 1 - dist / NEAR : 0;
      d.a += (target - d.a) * 0.18;
      if (Math.abs(d.ox) + Math.abs(d.oy) + Math.abs(d.vx) + Math.abs(d.vy) > 0.05 || Math.abs(target - d.a) > 0.01) moving = true;
    }
    draw();
    if (moving && visible) requestAnimationFrame(step);
    else running = false;
  }
  function wake() { if (!running && visible) { running = true; requestAnimationFrame(step); } }

  function toLocal(e) {
    var r = canvas.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top];
  }

  function onMove(e) {
    var p = toLocal(e), t = performance.now(), dt = Math.max(t - lastT, 8);
    if (lastT && px > -1e3) { pvx = (p[0] - px) / dt * 16; pvy = (p[1] - py) / dt * 16; }
    px = p[0]; py = p[1]; lastT = t;
    var speed = Math.sqrt(pvx * pvx + pvy * pvy);
    if (speed > 4) {
      var push = Math.min(speed, 60) / 60;
      for (var k = 0; k < dots.length; k++) {
        var d = dots[k], dx = d.hx - px, dy = d.hy - py;
        if (Math.abs(dx) > NEAR || Math.abs(dy) > NEAR) continue;
        var dist = Math.sqrt(dx * dx + dy * dy);
        if (dist >= NEAR) continue;
        var f = (1 - dist / NEAR) * push * 0.35;
        d.vx += pvx * f; d.vy += pvy * f;
      }
    }
    wake();
  }
  function onLeave() { px = py = -1e4; pvx = pvy = 0; lastT = 0; wake(); }

  function onDown(e) {
    var p = toLocal(e);
    if (p[1] < 0 || p[1] > h) return;
    for (var k = 0; k < dots.length; k++) {
      var d = dots[k], dx = d.hx - p[0], dy = d.hy - p[1];
      var dist = Math.sqrt(dx * dx + dy * dy);
      if (dist >= WAVE || dist < 1) continue;
      var f = (1 - dist / WAVE) * 9;
      d.vx += dx / dist * f; d.vy += dy / dist * f;
    }
    wake();
  }

  colors();
  layout();
  var resizeTimer;
  window.addEventListener("resize", function () {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(layout, 120);
  });
  document.addEventListener("themechange", function () { colors(); draw(); });
  if (still.matches) return;

  if ("IntersectionObserver" in window)
    new IntersectionObserver(function (es) { visible = es[0].isIntersecting; if (visible) wake(); }).observe(canvas);
  window.addEventListener("pointermove", onMove, { passive: true });
  document.documentElement.addEventListener("pointerleave", onLeave);
  window.addEventListener("pointerdown", onDown, { passive: true });
})();
