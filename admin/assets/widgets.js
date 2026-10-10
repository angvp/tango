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

  function showStatus(element, message) {
    element.textContent = message;
  }

  function formatJSON(textarea, status) {
    if (textarea.value.trim() === "") {
      showStatus(status, "Nothing to format.");
      return;
    }
    var pretty;
    try {
      pretty = JSON.stringify(JSON.parse(textarea.value), null, 2);
    } catch (err) {
      showStatus(status, "Not valid JSON: " + err.message);
      return;
    }
    textarea.value = pretty;
    showStatus(status, "Formatted.");
  }

  function setupJSON(root) {
    var textarea = root.querySelector("textarea");
    var button = root.querySelector("[data-json-format]");
    var status = root.querySelector("[data-json-status]");
    if (!textarea || !button || !status) {
      return;
    }
    button.hidden = false;
    button.addEventListener("click", function () {
      formatJSON(textarea, status);
    });
  }

  // The count text matches the server's: "n characters", or "n / max
  // characters" when the field is bounded. Array.from counts code points,
  // as Go's rune count does.
  function markdownCountText(length, max) {
    if (max > 0) {
      return length + " / " + max + " characters";
    }
    return length + " characters";
  }

  function setupMarkdownCount(textarea) {
    var count = document.getElementById(textarea.id + "-count");
    if (!count) {
      return;
    }
    var max = parseInt(textarea.getAttribute("maxlength"), 10);
    textarea.addEventListener("input", function () {
      var length = Array.from(textarea.value).length;
      count.textContent = markdownCountText(length, max);
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
  function readPreview(response) {
    return response
      .json()
      .catch(function () {
        return {};
      })
      .then(function (body) {
        if (!response.ok) {
          throw new Error(body.error || "Preview failed.");
        }
        if (typeof body.html !== "string") {
          throw new Error("Preview failed.");
        }
        return body.html;
      });
  }

  function requestPreview(textarea, button, frame, status) {
    var token = csrfToken(textarea);
    if (!token) {
      showStatus(status, "Preview unavailable. Reload the page and try again.");
      return;
    }
    button.disabled = true;
    showStatus(status, "Rendering preview...");
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
      .then(readPreview)
      .then(function (html) {
        frame.srcdoc = html;
        frame.hidden = false;
        showStatus(status, "Preview updated.");
      })
      .catch(function (err) {
        showStatus(status, err.message);
      })
      .finally(function () {
        button.disabled = false;
      });
  }

  // The Preview control appears only when the server rendered it, so
  // without this script the field stays a plain textarea. The textarea's
  // value is never changed here.
  function setupMarkdownPreview(textarea) {
    var button = document.getElementById(textarea.id + "-preview-button");
    var frame = document.getElementById(textarea.id + "-preview");
    var status = document.getElementById(textarea.id + "-preview-status");
    if (!button || !frame || !status) {
      return;
    }
    button.hidden = false;
    button.addEventListener("click", function () {
      requestPreview(textarea, button, frame, status);
    });
  }

  function setupMarkdown(textarea) {
    setupMarkdownCount(textarea);
    if (textarea.hasAttribute("data-preview-url")) {
      setupMarkdownPreview(textarea);
    }
  }

  register("json", setupJSON);
  register("markdown", setupMarkdown);
})();
