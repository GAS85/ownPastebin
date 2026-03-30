$(document).ready(function () {
  function replaceUrlParam(url, param, value) {
    if (value == null) {
      value = "";
    }

    var pattern = new RegExp("\\b(" + param + "=).*?(&|#|$)");
    if (url.search(pattern) >= 0) {
      return url.replace(pattern, "$1" + value + "$2");
    }

    url = url.replace(/[?#]$/, "");
    return url + (url.indexOf("?") > 0 ? "&" : "?") + param + "=" + value;
  }

  // Base for flash redirects — always the root (or uri_prefix root).
  // Fixed: original code redirected to /new which does not exist in the Go app.
  function flashBase() {
    return uri_prefix === "" ? "/" : uri_prefix + "/";
  }

  function resetLanguageSelector() {
    var url = new URL(document.location);
    var params = url.searchParams;
    var lang = params.get("lang");

    if (lang != null) {
      $("#language-selector").val(lang);
    } else {
      if ($("#pastebin-code-block").length) {
        $("#language-selector").val(
          $("#pastebin-code-block").prop("class").trim().split("-")[1],
        );
      }
    }
  }

  function getDefaultExpiryTime() {
    var expiry = $("#expiry-dropdown-btn").text().split("Expires: ")[1];
    return $("#expiry-dropdown a:contains('" + expiry + "')").attr("href");
  }

  function checkPasswordModal() {
    if ($("#password-modal").length) {
      $("#password-modal").modal("toggle");
    }
  }

  resetLanguageSelector();
  checkPasswordModal();
  init_plugins();

  var state = {
    expiry: getDefaultExpiryTime(),
    burn: 0,
  };

  $("#language-selector").change(function () {
    if ($("#pastebin-code-block").length) {
      $("#pastebin-code-block").attr(
        "class",
        "language-" + $("#language-selector").val(),
      );
      init_plugins();
    }
  });

  $("#remove-btn").on("click", function (event) {
    event.preventDefault();
    $("#deletion-modal").modal("show");
  });

  $("#deletion-confirm-btn").on("click", function (event) {
    event.preventDefault();

    $.ajax({
      url: window.location.pathname,
      type: "DELETE",
      success: function (result) {
        // Fixed: was uri_prefix + "/new" — Go app has no /new route
        var uri = flashBase();
        uri = replaceUrlParam(uri, "level", "info");
        uri = replaceUrlParam(uri, "glyph", "fas fa-info-circle");
        uri = replaceUrlParam(
          uri,
          "msg",
          "The paste has been successfully removed.",
        );
        window.location.href = encodeURI(uri);
      },
    });
  });

  $("#copy-btn").on("click", function (event) {
    event.preventDefault();

    $(".toolbar-item button").get(0).click();

    var $this = $(this);
    $this.text("Copied!");
    $this.attr("disabled", "disabled");

    setTimeout(function () {
      $this.text("Copy");
      $this.removeAttr("disabled");
    }, 800);
  });

  $("#send-btn").on("click", function (event) {
    event.preventDefault();

    var uri = uri_prefix === "" ? "/" : uri_prefix + "/";
    uri = replaceUrlParam(uri, "lang", $("#language-selector").val());
    uri = replaceUrlParam(uri, "ttl", state.expiry);
    uri = replaceUrlParam(uri, "burn", state.burn);

    var data = $("#content-textarea").val();
    var pass = $("#pastebin-password").val();

    if (pass.length > 0) {
      data = CryptoJS.AES.encrypt(data, pass).toString();
      uri = replaceUrlParam(uri, "encrypted", true);
    }

    $.ajax({
      url: uri,
      type: "POST",
      data: data,
      success: function (result) {
        // Fixed: was uri_prefix + "/new"; result is now a JSON object
        // {url, id, lang} — extract the paste URL from it.
        var pasteUrl =
          typeof result === "object" ? result.url : JSON.parse(result).url;
        var redirect = flashBase();
        redirect = replaceUrlParam(redirect, "level", "success");
        redirect = replaceUrlParam(redirect, "glyph", "fas fa-check");
        redirect = replaceUrlParam(
          redirect,
          "msg",
          "The paste has been successfully created:",
        );
        redirect = replaceUrlParam(redirect, "url", pasteUrl);
        window.location.href = encodeURI(redirect);
      },
    });
  });

  $("#expiry-dropdown a").click(function (event) {
    event.preventDefault();

    state.expiry = $(this).attr("href");
    $("#expiry-dropdown-btn").text("Expires: " + this.innerHTML);
  });

  $("#burn-dropdown a").click(function (event) {
    event.preventDefault();

    state.burn = $(this).attr("href");
    $("#burn-dropdown-btn").text("Burn: " + this.innerHTML);
  });

  $("#password-modal").on("shown.bs.modal", function () {
    $("#modal-password").trigger("focus");
  });

  $("#password-modal form").submit(function (event) {
    event.preventDefault();
    $("#decrypt-btn").click();
  });

  $("#decrypt-btn").click(function (event) {
    var pass = $("#modal-password").val();
    var data = "";

    if ($("#pastebin-code-block").length) {
      data = $("#pastebin-code-block").text();
    } else {
      data = $("#content-textarea").text();
    }

    var decrypted = CryptoJS.AES.decrypt(data, pass).toString(
      CryptoJS.enc.Utf8,
    );
    if (decrypted.length == 0) {
      $("#modal-alert").removeClass("collapse");
    } else {
      if ($("#pastebin-code-block").length) {
        $("#pastebin-code-block").text(decrypted);
        init_plugins();
      } else {
        $("#content-textarea").text(decrypted);
      }

      $("#modal-close-btn").click();
      $("#modal-alert").alert("close");
    }
  });
});

// Used to toggle the menu on smaller screens when clicking on the menu button
function openNav() {
  var x = document.getElementById("mobileMenu");
  if (x.className.indexOf("w3-show") == -1) {
    x.className += " w3-show";
  } else {
    x.className = x.className.replace(" w3-show", "");
  }
}
