// Bulk delete for the admin changelist.
//
// The server renders every control hidden. This script reveals them, so a
// browser without JavaScript never shows a delete control that cannot work.
// Confirming submits the checked keys in one POST; the server validates the
// selection again.
(function () {
  "use strict";

  function text(root, name, vars) {
    var value = root.getAttribute("data-msg-" + name) || "";
    Object.keys(vars || {}).forEach(function (key) {
      value = value.split("{" + key + "}").join(vars[key]);
    });
    return value;
  }

  function setup(root) {
    var boxes = Array.prototype.slice.call(root.querySelectorAll("[data-bulk-pk]"));
    var selectAlls = Array.prototype.slice.call(root.querySelectorAll("[data-bulk-select-all]"));
    var bar = root.querySelector("[data-bulk-bar]");
    var counter = root.querySelector("[data-bulk-count]");
    var open = root.querySelector("[data-bulk-open]");
    var dialog = root.querySelector("[data-bulk-dialog]");
    var form = root.querySelector("[data-bulk-form]");
    var title = root.querySelector("[data-bulk-title]");
    var list = root.querySelector("[data-bulk-list]");
    var count = root.querySelector("[data-bulk-form-count]");
    var cancel = root.querySelector("[data-bulk-cancel]");
    var confirm = root.querySelector("[data-bulk-confirm]");
    var limit = parseInt(root.getAttribute("data-bulk-limit"), 10);
    if (!boxes.length || !bar || !dialog || !form || typeof dialog.showModal !== "function") {
      return;
    }

    root.querySelectorAll("[data-bulk-cell]").forEach(function (cell) {
      cell.hidden = false;
    });
    bar.hidden = false;

    function checked() {
      return boxes.filter(function (box) {
        return box.checked;
      });
    }

    function refresh() {
      var picked = checked().length;
      open.disabled = picked === 0;
      if (picked === 0) {
        counter.textContent = root.getAttribute("data-msg-none") || "";
        open.setAttribute("aria-describedby", counter.id);
      } else {
        counter.textContent = text(root, "selected", { count: picked });
        open.removeAttribute("aria-describedby");
      }
      selectAlls.forEach(function (selectAll) {
        selectAll.checked = picked === boxes.length;
        selectAll.indeterminate = picked > 0 && picked < boxes.length;
      });
    }

    boxes.forEach(function (box) {
      box.addEventListener("change", refresh);
    });
    selectAlls.forEach(function (selectAll) {
      selectAll.addEventListener("change", function () {
        boxes.forEach(function (box) {
          box.checked = selectAll.checked;
        });
        refresh();
      });
    });

    open.addEventListener("click", function () {
      var picked = checked();
      form.querySelectorAll('input[name="pk"]').forEach(function (input) {
        input.remove();
      });
      list.textContent = "";
      picked.forEach(function (box, index) {
        var input = document.createElement("input");
        input.type = "hidden";
        input.name = "pk";
        input.value = box.getAttribute("data-bulk-pk");
        form.appendChild(input);
        if (index < limit) {
          var item = document.createElement("li");
          item.textContent = box.getAttribute("data-bulk-identity");
          list.appendChild(item);
        }
      });
      if (picked.length > limit) {
        var more = document.createElement("li");
        var rest = picked.length - limit;
        more.textContent = rest === 1 ? text(root, "more-one") : text(root, "more-many", { count: rest });
        list.appendChild(more);
      }
      count.value = String(picked.length);
      title.textContent = picked.length === 1 ? text(root, "title-one") : text(root, "title-many", { count: picked.length });
      confirm.textContent = picked.length === 1
        ? root.getAttribute("data-msg-confirm-one") || ""
        : text(root, "confirm-many", { count: picked.length });
      confirm.disabled = false;
      dialog.showModal();
    });

    cancel.addEventListener("click", function () {
      dialog.close();
    });
    form.addEventListener("submit", function () {
      confirm.disabled = true;
    });
    window.addEventListener("pageshow", refresh);
    refresh();
  }

  document.querySelectorAll("[data-bulk-delete]").forEach(setup);
})();
