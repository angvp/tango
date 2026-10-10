// Behaviour for admin widgets, shared by every widget that needs it.
//
// A widget marks its markup with data-tango-widget="<name>". A setup
// function registered under that name runs once for each such element in
// the document. To add a widget's behaviour, register a setup function
// below; the widget's markup and its data attributes do the rest.
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

  register("json", setupJSON);
})();
