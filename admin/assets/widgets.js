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

  function showStatus(element, message) {
    element.textContent = message;
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

    tablist.hidden = false;
    return function (name) {
      select(name, false);
    };
  }

  // formatJSON returns the indented form of text, or the parse error.
  function formatJSON(text) {
    if (text.trim() === "") {
      return { empty: true };
    }
    try {
      return { text: JSON.stringify(JSON.parse(text), null, 2) };
    } catch (err) {
      return { error: err.message };
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

    function showFormatted() {
      var result = formatJSON(textarea.value);
      formatted = "";
      apply.hidden = true;
      view.removeAttribute("data-invalid");
      if (result.empty) {
        view.textContent = "";
        showStatus(status, msg(root, "nothing", "Nothing to format."));
      } else if (result.error) {
        view.setAttribute("data-invalid", "");
        view.textContent = msg(root, "invalid-detail", "Not valid JSON: {error}", { error: result.error });
        showStatus(status, msg(root, "invalid", "Not valid JSON."));
      } else {
        formatted = result.text;
        view.textContent = result.text;
        apply.hidden = result.text === textarea.value;
        showStatus(status, "");
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
      showStatus(status, msg(root, "applied", "Formatting applied."));
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
      showStatus(status, msg(textarea, "preview-unavailable", "Preview unavailable. Reload the page and try again."));
      return;
    }
    var ticket = current.next();
    showStatus(status, msg(textarea, "preview-rendering", "Rendering preview..."));
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
        showStatus(status, "");
      })
      .catch(function (err) {
        if (current.is(ticket)) {
          showStatus(status, err.message);
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
  focusInvalidControl();
})();
