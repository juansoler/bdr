/* BDR Self-Hosted — interacciones mínimas, sin dependencias.
   Todo es mejora progresiva: sin JS la página se lee y se navega igual. */
(function () {
  "use strict";

  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  /* ---------------------------------------------------------- menú móvil */
  var toggle = document.querySelector(".nav-toggle");
  var nav = document.getElementById("nav");

  if (toggle && nav) {
    toggle.addEventListener("click", function () {
      var open = toggle.getAttribute("aria-expanded") === "true";
      toggle.setAttribute("aria-expanded", String(!open));
      toggle.setAttribute("aria-label", open ? "Abrir menú" : "Cerrar menú");
      nav.classList.toggle("is-open", !open);
    });

    nav.addEventListener("click", function (event) {
      if (event.target.closest("a")) {
        toggle.setAttribute("aria-expanded", "false");
        toggle.setAttribute("aria-label", "Abrir menú");
        nav.classList.remove("is-open");
      }
    });
  }

  /* ---------------------------------------------------------- copiar código */
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    // Fallback para http:// o navegadores antiguos.
    return new Promise(function (resolve, reject) {
      var area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      try {
        document.execCommand("copy") ? resolve() : reject(new Error("copy"));
      } catch (error) {
        reject(error);
      } finally {
        document.body.removeChild(area);
      }
    });
  }

  document.querySelectorAll("[data-copy]").forEach(function (button) {
    button.addEventListener("click", function () {
      var target = document.getElementById(button.getAttribute("data-copy"));
      if (!target) return;

      copyText(target.textContent.trim()).then(
        function () {
          var original = button.textContent;
          button.textContent = "Copiado";
          button.classList.add("is-done");
          window.setTimeout(function () {
            button.textContent = original;
            button.classList.remove("is-done");
          }, 1800);
        },
        function () {
          button.textContent = "Selecciona y copia";
          window.setTimeout(function () {
            button.textContent = "Copiar";
          }, 2200);
        }
      );
    });
  });

  /* ---------------------------------------------------------- pista de scroll en código */
  // Los bloques con comandos largos hacen scroll horizontal en pantallas
  // estrechas. Un degradado en el borde derecho avisa de que hay más contenido
  // y se retira al llegar al final.
  var codeBlocks = Array.prototype.slice.call(document.querySelectorAll(".code"));

  var updateScrollHints = function () {
    codeBlocks.forEach(function (block) {
      var pre = block.querySelector("pre");
      var head = block.querySelector(".code-head");
      if (!pre) return;
      if (head) block.style.setProperty("--head-h", head.offsetHeight + "px");
      var scrollable = pre.scrollWidth > pre.clientWidth + 1;
      block.classList.toggle("can-scroll", scrollable);
      block.classList.toggle(
        "at-end",
        scrollable && pre.scrollLeft + pre.clientWidth >= pre.scrollWidth - 2
      );
    });
  };

  codeBlocks.forEach(function (block) {
    var pre = block.querySelector("pre");
    if (pre) pre.addEventListener("scroll", updateScrollHints, { passive: true });
  });
  window.addEventListener("resize", updateScrollHints);
  window.addEventListener("load", updateScrollHints);
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(updateScrollHints);
  }
  updateScrollHints();

  /* ---------------------------------------------------------- reveal */
  var revealables = Array.prototype.slice.call(document.querySelectorAll(".reveal"));

  if (reduceMotion || !("IntersectionObserver" in window)) {
    revealables.forEach(function (el) { el.classList.add("is-in"); });
  } else {
    var pendingReveal = revealables.slice();

    var reveal = function (el) {
      if (!el.classList.contains("is-in")) el.classList.add("is-in");
      var index = pendingReveal.indexOf(el);
      if (index !== -1) pendingReveal.splice(index, 1);
    };

    var observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (!entry.isIntersecting) return;
          reveal(entry.target);
          observer.unobserve(entry.target);
        });
      },
      { rootMargin: "0px 0px -8% 0px", threshold: 0.08 }
    );
    revealables.forEach(function (el) { observer.observe(el); });

    // Red de seguridad: al saltar con un ancla (o cargar con #seccion) hay
    // elementos que nunca llegan a intersecar. Lo que queda por encima del
    // viewport se marca visible para que no quede oculto al volver.
    var scheduled = false;
    var sweep = function () {
      scheduled = false;
      var limit = window.innerHeight * 0.96;
      pendingReveal.slice().forEach(function (el) {
        if (el.getBoundingClientRect().top < limit) {
          reveal(el);
          observer.unobserve(el);
        }
      });
    };
    var onScroll = function () {
      if (scheduled) return;
      scheduled = true;
      window.requestAnimationFrame(sweep);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    window.addEventListener("load", onScroll);
    sweep();
  }

  /* ---------------------------------------------------------- año del footer */
  var year = document.getElementById("year");
  if (year) year.textContent = String(new Date().getFullYear());

  /* ---------------------------------------------------------- sección activa */
  if ("IntersectionObserver" in window) {
    var links = Array.prototype.slice.call(document.querySelectorAll('.nav a[href^="#"]'));
    var byId = {};
    links.forEach(function (link) {
      byId[link.getAttribute("href").slice(1)] = link;
    });

    var sections = Object.keys(byId)
      .map(function (id) { return document.getElementById(id); })
      .filter(Boolean);

    var activeObserver = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          var link = byId[entry.target.id];
          if (!link) return;
          if (entry.isIntersecting) {
            links.forEach(function (other) { other.removeAttribute("aria-current"); });
            link.setAttribute("aria-current", "true");
          }
        });
      },
      { rootMargin: "-45% 0px -50% 0px" }
    );

    sections.forEach(function (section) { activeObserver.observe(section); });
  }
})();
