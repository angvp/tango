// Sidebar behaviour for the admin layout: the menu button opens the sidebar
// and the backdrop closes it. Elements opt in with data-admin-nav-open and
// data-admin-nav-close, so the layout's markup carries no inline handler.
(function () {
  "use strict";

  var sidebar = document.getElementById("sidebar");
  var backdrop = document.getElementById("sidebar-backdrop");
  if (!sidebar || !backdrop) {
    return;
  }

  function open() {
    sidebar.classList.add("sidebar-open");
    backdrop.classList.add("sidebar-backdrop-open");
  }

  function close() {
    sidebar.classList.remove("sidebar-open");
    backdrop.classList.remove("sidebar-backdrop-open");
  }

  window.tangoAdminNav = { open: open, close: close };

  document.querySelectorAll("[data-admin-nav-open]").forEach(function (element) {
    element.addEventListener("click", open);
  });
  document.querySelectorAll("[data-admin-nav-close]").forEach(function (element) {
    element.addEventListener("click", close);
  });
})();
