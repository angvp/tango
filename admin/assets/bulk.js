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
    var selectAll = root.querySelector("[data-bulk-select-all]");
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
    if (!boxes.length || !bar || !dialog || !form || typeof dialog.showModal !== "function") {
      return;
    }

    root.querySelectorAll("[data-bulk-cell]").forEach(function (cell) {
      cell.hidden = false;
    });

    function checked() {
      return boxes.filter(function (box) {
        return box.checked;
      });
    }

    function refresh() {
      var picked = checked().length;
      bar.hidden = picked === 0;
      open.disabled = picked === 0;
      counter.textContent = picked === 0 ? "" : text(root, "selected", { count: picked });
      if (selectAll) {
        selectAll.checked = picked === boxes.length;
        selectAll.indeterminate = picked > 0 && picked < boxes.length;
      }
    }

    boxes.forEach(function (box) {
      box.addEventListener("change", refresh);
    });
    if (selectAll) {
      selectAll.addEventListener("change", function () {
        boxes.forEach(function (box) {
          box.checked = selectAll.checked;
        });
        refresh();
      });
    }

    open.addEventListener("click", function () {
      var picked = checked();
      if (picked.length === 0) {
        return;
      }
      form.querySelectorAll('input[name="pk"]').forEach(function (input) {
        input.remove();
      });
      list.textContent = "";
      picked.forEach(function (box) {
        var input = document.createElement("input");
        input.type = "hidden";
        input.name = "pk";
        input.value = box.getAttribute("data-bulk-pk");
        form.appendChild(input);
        var item = document.createElement("li");
        item.textContent = box.getAttribute("data-bulk-identity");
        list.appendChild(item);
      });
      count.value = String(picked.length);
      title.textContent = text(root, "title", { count: picked.length });
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
