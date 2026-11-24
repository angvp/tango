// Behaviour for admin widgets, shared by every widget that needs it.
//
// Built-in admin widgets mark their markup with data-tango-widget="<name>".
// A setup function registered under that name runs once for each such
// element in the document. Only this package's own widgets register here.
(function () {
  "use strict";

  // Another copy of this script already ran (a form with several widgets
  // each emits a script tag for it), so there is nothing more to do.
  if (window.tangoAdminWidgets) {
    return;
  }

  function setupElements(name, setup) {
    var selector = '[data-tango-widget="' + name + '"]';
    document.querySelectorAll(selector).forEach(setup);
  }

  function register(name, setup) {
    setupElements(name, setup);
  }

  window.tangoAdminWidgets = { register: register };

  // msg returns the message a widget's server markup carried in
  // data-msg-<name>, or fallback (the English text) when it carried none.
  // {name} placeholders are replaced from vars.
  function msg(element, name, fallback, vars) {
    var text = element.getAttribute("data-msg-" + name);
    if (text === null) {
      text = fallback;
    }
    return text.replace(/\{(\w+)\}/g, function (whole, key) {
      return vars && Object.prototype.hasOwnProperty.call(vars, key) ? String(vars[key]) : whole;
    });
  }

  // showStatus sets a status line's text. invalid switches it to the field
  // error style, so a problem reads as one and not as a hint.
  function showStatus(element, message, invalid) {
    element.textContent = message;
    element.classList.toggle("field-error", invalid);
    element.classList.toggle("hint-small", !invalid);
  }

  // setupTabs wires an ARIA tab list: click, Left/Right/Home/End to move
  // between tabs, and one panel visible at a time. onActivate(name) runs
  // after a tab becomes active. It returns select(name) so a widget can
  // switch tabs itself. The tab list ships hidden, so without this script
  // the field stays a plain textarea.
  function setupTabs(tablist, onActivate) {
    var tabs = Array.prototype.slice.call(tablist.querySelectorAll('[role="tab"]'));
    if (tabs.length === 0) {
      return function () {};
    }

    function panelOf(tab) {
      return document.getElementById(tab.getAttribute("aria-controls"));
    }

    function select(name, focus) {
      tabs.forEach(function (tab) {
        var active = tab.getAttribute("data-tab") === name;
        tab.setAttribute("aria-selected", active ? "true" : "false");
        tab.tabIndex = active ? 0 : -1;
        var panel = panelOf(tab);
        if (panel) {
          panel.hidden = !active;
        }
        if (active && focus) {
          tab.focus();
        }
      });
      onActivate(name);
    }

    tabs.forEach(function (tab, index) {
      tab.addEventListener("click", function () {
        select(tab.getAttribute("data-tab"), false);
      });
      tab.addEventListener("keydown", function (event) {
        var target = -1;
        if (event.key === "ArrowRight") {
          target = (index + 1) % tabs.length;
        } else if (event.key === "ArrowLeft") {
          target = (index - 1 + tabs.length) % tabs.length;
        } else if (event.key === "Home") {
          target = 0;
        } else if (event.key === "End") {
          target = tabs.length - 1;
        }
        if (target >= 0) {
          event.preventDefault();
          select(tabs[target].getAttribute("data-tab"), true);
        }
      });
    });

    // Without this, every panel stays visible until the first tab is clicked.
    // The tab the markup marks selected is shown, and the others are hidden.
    var initial = tabs.filter(function (tab) {
      return tab.getAttribute("aria-selected") === "true";
    })[0];
    if (initial) {
      select(initial.getAttribute("data-tab"), false);
    }
    tablist.hidden = false;
    return function (name) {
      select(name, false);
    };
  }

  // formatJSON returns the indented form of text, or {invalid: true}. The
  // parser's own message is not used: it quotes part of the document.
  function formatJSON(text) {
    if (text.trim() === "") {
      return { empty: true };
    }
    try {
      return { text: JSON.stringify(JSON.parse(text), null, 2) };
    } catch (err) {
      return { invalid: true };
    }
  }

  // The Formatted tab is a view: it shows the textarea's text indented, or
  // why it cannot be. The textarea changes only when the user chooses to
  // apply the formatting.
  function setupJSON(root) {
    var textarea = root.querySelector("textarea");
    var tablist = root.querySelector("[data-widget-tabs]");
    var view = root.querySelector("[data-json-formatted]");
    var apply = root.querySelector("[data-json-apply]");
    var status = root.querySelector("[data-json-status]");
    if (!textarea || !tablist || !view || !apply || !status) {
      return;
    }
    var formatted = "";

    // One message, in the status line: the server sends the sentence in
    // data-msg-invalid-detail, and the Formatted box stays empty. The line
    // takes the field error style while the text is invalid.
    function showFormatted() {
      var result = formatJSON(textarea.value);
      formatted = "";
      apply.hidden = true;
      view.textContent = "";
      if (result.empty) {
        showStatus(status, msg(root, "nothing", "Nothing to format."), false);
      } else if (result.invalid) {
        showStatus(status, msg(root, "invalid-detail", ""), true);
      } else {
        formatted = result.text;
        view.textContent = result.text;
        apply.hidden = result.text === textarea.value;
        showStatus(status, "", false);
      }
    }

    var select = setupTabs(tablist, function (name) {
      if (name === "view") {
        showFormatted();
      }
    });
    apply.addEventListener("click", function () {
      textarea.value = formatted;
      select("edit");
      textarea.focus();
      showStatus(status, msg(root, "applied", "Formatting applied."), false);
    });
  }

  // The count text matches the server's: "n characters", or "n / max
  // characters" when the field is bounded. Array.from counts code points,
  // as Go's rune count does.
  function markdownCountText(textarea, length, max) {
    if (max > 0) {
      return msg(textarea, "count-max", "{count} / {max} characters", { count: length, max: max });
    }
    return msg(textarea, "count", "{count} characters", { count: length });
  }

  function setupMarkdownCount(textarea) {
    var count = document.getElementById(textarea.id + "-count");
    if (!count) {
      return;
    }
    var max = parseInt(textarea.getAttribute("maxlength"), 10);
    textarea.addEventListener("input", function () {
      var length = Array.from(textarea.value).length;
      count.textContent = markdownCountText(textarea, length, max);
    });
  }

  // The CSRF token is the one the enclosing form submits.
  function csrfToken(textarea) {
    var field = textarea.form && textarea.form.elements.namedItem("csrf_token");
    return field ? field.value : "";
  }

  // readPreview turns the response into the HTML to show, or throws the
  // message to show instead. A response that is not our JSON (for example a
  // redirect to the login page) gets the generic message.
  function readPreview(textarea, response) {
    return response
      .json()
      .catch(function () {
        return {};
      })
      .then(function (body) {
        if (!response.ok) {
          throw new Error(body.error || msg(textarea, "preview-failed", "Preview failed."));
        }
        if (typeof body.html !== "string") {
          throw new Error(msg(textarea, "preview-failed", "Preview failed."));
        }
        return body.html;
      });
  }

  function requestPreview(textarea, frame, status, current) {
    var token = csrfToken(textarea);
    if (!token) {
      showStatus(status, msg(textarea, "preview-unavailable", "Preview unavailable. Reload the page and try again."), true);
      return;
    }
    var ticket = current.next();
    showStatus(status, msg(textarea, "preview-rendering", "Rendering preview..."), false);
    fetch(textarea.getAttribute("data-preview-url"), {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "Accept": "application/json",
        "X-CSRF-Token": token
      },
      body: textarea.value
    })
      .then(function (response) {
        return readPreview(textarea, response);
      })
      .then(function (html) {
        if (!current.is(ticket)) {
          return;
        }
        frame.srcdoc = html;
        showStatus(status, "", false);
      })
      .catch(function (err) {
        if (current.is(ticket)) {
          showStatus(status, err.message, true);
        }
      });
  }

  // requestCounter lets a slow response be ignored once a newer request has
  // started.
  function requestCounter() {
    var n = 0;
    return {
      next: function () {
        n += 1;
        return n;
      },
      is: function (ticket) {
        return ticket === n;
      }
    };
  }

  // The Write and Preview tabs appear only when the server rendered them,
  // so without this script the field stays a plain textarea. Opening the
  // Preview tab renders the current text; the textarea's value is never
  // changed here.
  function setupMarkdownPreview(textarea) {
    var tablist = document.getElementById(textarea.id + "-tab-write");
    tablist = tablist && tablist.parentElement;
    var frame = document.getElementById(textarea.id + "-preview");
    var status = document.getElementById(textarea.id + "-preview-status");
    if (!tablist || !frame || !status) {
      return;
    }
    var current = requestCounter();
    setupTabs(tablist, function (name) {
      if (name === "preview") {
        requestPreview(textarea, frame, status, current);
      }
    });
  }

  function setupMarkdown(textarea) {
    setupMarkdownCount(textarea);
    if (textarea.hasAttribute("data-preview-url")) {
      setupMarkdownPreview(textarea);
    }
  }


  // lookupDebounce is how long typing pauses before a search is sent.
  var lookupDebounce = 250;

  // setupLookup upgrades a lookup field's primary key input to an editable
  // combobox (ARIA 1.2, list autocomplete). The server's input becomes the
  // hidden field that is submitted; a visible text input takes its place and
  // searches as the person types. Without this script the key input stays a
  // plain text input that takes the key itself.
  function setupLookup(keyInput) {
    var url = keyInput.getAttribute("data-lookup-url");
    if (!url || !window.fetch || !window.AbortController) {
      return;
    }
    var id = keyInput.id;
    var optional = keyInput.hasAttribute("data-lookup-optional");
    var selectedLabel = keyInput.value === "" ? "" : (keyInput.getAttribute("data-lookup-label") || keyInput.value);
    var fieldLabel = document.querySelector('label[for="' + id + '"]');

    var input = document.createElement("input");
    input.type = "text";
    input.id = id;
    input.className = "input";
    input.value = selectedLabel;
    input.autocomplete = "off";
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-controls", id + "-listbox");
    ["aria-invalid", "aria-describedby"].forEach(function (name) {
      if (keyInput.hasAttribute(name)) {
        input.setAttribute(name, keyInput.getAttribute(name));
      }
    });

    var list = document.createElement("ul");
    list.id = id + "-listbox";
    list.className = "combobox-list";
    list.setAttribute("role", "listbox");
    if (fieldLabel) {
      list.setAttribute("aria-label", fieldLabel.textContent);
    }
    list.hidden = true;

    var remove = document.createElement("button");
    remove.type = "button";
    remove.className = "btn-ghost";
    remove.textContent = msg(keyInput, "remove", "Remove");

    var status = document.createElement("div");
    status.className = "sr-only";
    status.setAttribute("role", "status");
    var alertBox = document.createElement("p");
    alertBox.className = "field-error";
    alertBox.setAttribute("role", "alert");
    alertBox.hidden = true;

    var row = document.createElement("div");
    row.className = "combobox-row";
    row.appendChild(input);
    if (optional) {
      row.appendChild(remove);
    }
    var box = document.createElement("div");
    box.className = "combobox";
    box.appendChild(row);
    box.appendChild(list);
    box.appendChild(status);
    box.appendChild(alertBox);

    keyInput.type = "hidden";
    keyInput.removeAttribute("id");
    keyInput.removeAttribute("class");
    keyInput.removeAttribute("aria-invalid");
    keyInput.removeAttribute("aria-describedby");
    var current = document.getElementById(id + "-current");
    if (current) {
      current.remove();
    }
    keyInput.parentNode.insertBefore(box, keyInput.nextSibling);

    var items = [];
    var active = -1;
    var dirty = false;
    var controller = null;
    var timer = null;
    var counter = requestCounter();
    var choosing = false;

    function announce(text) {
      status.textContent = "";
      status.textContent = text;
    }

    function showError(message) {
      alertBox.textContent = message;
      alertBox.hidden = message === "";
    }

    function updateRemove() {
      remove.hidden = keyInput.value === "";
    }

    function setActive(index) {
      var options = list.querySelectorAll('[role="option"]');
      options.forEach(function (option, i) {
        option.classList.toggle("combobox-option-active", i === index);
      });
      active = index;
      if (index >= 0 && options[index]) {
        input.setAttribute("aria-activedescendant", options[index].id);
        options[index].scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    }

    function close() {
      list.hidden = true;
      input.setAttribute("aria-expanded", "false");
      setActive(-1);
    }

    function cancelSearch() {
      clearTimeout(timer);
      counter.next();
      if (controller) {
        controller.abort();
        controller = null;
      }
    }

    function choose(item) {
      cancelSearch();
      keyInput.value = item.value;
      selectedLabel = item.text;
      input.value = item.text;
      dirty = false;
      close();
      showError("");
      updateRemove();
      announce(msg(keyInput, "selected", "{label} selected.", { label: item.text }));
      keyInput.dispatchEvent(new Event("change", { bubbles: true }));
    }

    function clearSelection() {
      cancelSearch();
      keyInput.value = "";
      selectedLabel = "";
      input.value = "";
      dirty = false;
      close();
      showError("");
      updateRemove();
      announce(msg(keyInput, "cleared", "Selection removed."));
      keyInput.dispatchEvent(new Event("change", { bubbles: true }));
      input.focus();
    }

    function restore() {
      cancelSearch();
      input.value = selectedLabel;
      dirty = false;
      close();
    }

    function addNote(text) {
      var note = document.createElement("li");
      note.className = "combobox-note";
      note.setAttribute("role", "presentation");
      note.textContent = text;
      list.appendChild(note);
    }

    function render(answer, activateFirst) {
      items = answer.items || [];
      list.textContent = "";
      items.forEach(function (item, i) {
        var option = document.createElement("li");
        option.id = id + "-option-" + i;
        option.className = "combobox-option";
        option.setAttribute("role", "option");
        option.setAttribute("aria-selected", item.value === keyInput.value ? "true" : "false");
        option.textContent = item.text;
        option.addEventListener("mousedown", function (event) {
          event.preventDefault();
        });
        option.addEventListener("click", function () {
          choose(item);
          input.focus();
        });
        list.appendChild(option);
      });
      if (items.length === 0) {
        addNote(msg(keyInput, "none", "No results."));
        announce(msg(keyInput, "none", "No results."));
      } else {
        announce(msg(keyInput, "results", "{count} results. Use the up and down arrow keys to choose.", { count: items.length }));
      }
      if (answer.more) {
        addNote(msg(keyInput, "more", "More results. Type to narrow."));
      }
      list.hidden = false;
      input.setAttribute("aria-expanded", "true");
      setActive(activateFirst && items.length > 0 ? 0 : -1);
    }

    function search(activateFirst) {
      cancelSearch();
      var ticket = counter.next();
      controller = new AbortController();
      var query = dirty ? input.value : "";
      fetch(url + "?q=" + encodeURIComponent(query), {
        credentials: "same-origin",
        cache: "no-store",
        headers: { "Accept": "application/json" },
        signal: controller.signal
      })
        .then(function (response) {
          if (!response.ok) {
            throw new Error("lookup failed");
          }
          return response.json();
        })
        .then(function (answer) {
          if (counter.is(ticket)) {
            showError("");
            render(answer, activateFirst);
          }
        })
        .catch(function (err) {
          if (err && err.name === "AbortError") {
            return;
          }
          if (counter.is(ticket)) {
            close();
            showError(msg(keyInput, "error", "Could not load results."));
          }
        });
    }

    function move(step) {
      if (items.length === 0) {
        return;
      }
      setActive((active + step + items.length) % items.length);
    }

    input.addEventListener("input", function () {
      dirty = true;
      showError("");
      clearTimeout(timer);
      timer = setTimeout(function () {
        search(false);
      }, lookupDebounce);
    });
    function openList() {
      if (list.hidden) {
        search(false);
      }
    }
    input.addEventListener("focus", openList);
    input.addEventListener("click", openList);
    input.addEventListener("blur", restore);
    input.addEventListener("keydown", function (event) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        if (list.hidden) {
          search(true);
        } else {
          move(1);
        }
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        if (!list.hidden) {
          move(-1);
        }
      } else if (event.key === "Enter") {
        if (!list.hidden && active >= 0 && items[active]) {
          event.preventDefault();
          choosing = true;
          choose(items[active]);
        }
      } else if (event.key === "Escape") {
        if (!list.hidden) {
          event.preventDefault();
          event.stopPropagation();
          restore();
        }
      }
    });
    // Choosing with Enter must not also submit the form, whatever order the
    // browser reports the key's events in.
    input.addEventListener("keypress", function (event) {
      if (event.key === "Enter" && choosing) {
        event.preventDefault();
      }
    });
    input.addEventListener("keyup", function (event) {
      if (event.key === "Enter") {
        choosing = false;
      }
    });
    remove.addEventListener("click", clearSelection);
    updateRemove();
  }

  // A failed save with one field problem has no summary to take focus, so the
  // invalid control takes it. The server marks the control aria-invalid.
  function focusInvalidControl() {
    if (document.getElementById("form-summary")) {
      return;
    }
    var invalid = document.querySelector('[aria-invalid="true"]');
    if (invalid) {
      invalid.focus();
    }
  }

  register("json", setupJSON);
  register("markdown", setupMarkdown);
  register("lookup", setupLookup);
  focusInvalidControl();
})();
