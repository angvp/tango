// Sidebar behaviour for the admin layout: the menu button opens the sidebar
// as a drawer, and the Close button, the backdrop and Escape close it.
// Elements opt in with data-admin-nav-open and data-admin-nav-close, so the
// layout's markup carries no inline handler. Focus moves into the drawer on
// open and returns to the menu button on close; while the drawer is open the
// page behind it, the skip link included, is inert and Tab stays inside the
// drawer. The script marks the document with nav-js; the stylesheet applies the
// phone drawer only under that mark, so without the script the model list
// stays in the page.
(function () {
  "use strict";

  var sidebar = document.getElementById("sidebar");
  var backdrop = document.getElementById("sidebar-backdrop");
  if (!sidebar || !backdrop) {
    return;
  }

  document.documentElement.classList.add("nav-js");

  var main = document.getElementById("main");
  var skipLink = document.querySelector(".skip-link");
  var openers = document.querySelectorAll("[data-admin-nav-open]");
  var closeButton = sidebar.querySelector("button[data-admin-nav-close]");
  var desktop = window.matchMedia("(min-width: 768px)");
  var opener = null;

  function isOpen() {
    return sidebar.classList.contains("sidebar-open");
  }

  function setExpanded(value) {
    openers.forEach(function (element) {
      element.setAttribute("aria-expanded", value ? "true" : "false");
    });
  }

  function setBackgroundInert(value) {
    [main, skipLink].forEach(function (element) {
      if (element) {
        element.inert = value;
      }
    });
  }

  function open() {
    if (isOpen() || desktop.matches) {
      return;
    }
    opener = document.activeElement && document.activeElement.hasAttribute("data-admin-nav-open")
      ? document.activeElement
      : openers[0] || null;
    sidebar.classList.add("sidebar-open");
    backdrop.classList.add("sidebar-backdrop-open");
    setExpanded(true);
    setBackgroundInert(true);
    if (closeButton) {
      closeButton.focus();
    }
  }

  function close() {
    if (!isOpen()) {
      return;
    }
    sidebar.classList.remove("sidebar-open");
    backdrop.classList.remove("sidebar-backdrop-open");
    setExpanded(false);
    setBackgroundInert(false);
    if (opener && !desktop.matches) {
      opener.focus();
    }
  }

  function trapTab(event) {
    var focusable = sidebar.querySelectorAll("a[href], button:not([disabled]), input:not([disabled])");
    if (focusable.length === 0) {
      return;
    }
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  window.tangoAdminNav = { open: open, close: close };

  openers.forEach(function (element) {
    element.addEventListener("click", open);
  });
  document.querySelectorAll("[data-admin-nav-close]").forEach(function (element) {
    element.addEventListener("click", close);
  });
  document.addEventListener("keydown", function (event) {
    if (!isOpen()) {
      return;
    }
    if (event.key === "Escape") {
      close();
    } else if (event.key === "Tab") {
      trapTab(event);
    }
  });
  desktop.addEventListener("change", function (event) {
    if (event.matches) {
      close();
    }
  });
})();
