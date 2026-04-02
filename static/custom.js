/**
 * custom.js — vanilla JS, Web Crypto API (SubtleCrypto).
 *
 * Encryption scheme: AES-256-GCM with PBKDF2-SHA-256 key derivation.
 *
 * Wire format (stored as Base64, prefixed "v2:"):
 *   v2:<base64( salt[16] + iv[12] + ciphertext + gcmTag[16] )>
 *
 *   - Salt:       16 random bytes  — for PBKDF2
 *   - IV:         12 random bytes  — for AES-GCM nonce
 *   - PBKDF2:     SHA-256, 200 000 iterations, 256-bit key
 *   - Cipher:     AES-256-GCM (authenticated, no separate HMAC needed)
 *   - GCM tag:    appended by SubtleCrypto automatically (last 16 bytes)
 *
 * Decrypt on the command line (requires OpenSSL ≥ 3.x for -pbkdf2 + GCM):
 *
 *   PAYLOAD="v2:..."                          # paste the stored value
 *   PASS="your-password"
 *   RAW=$(echo "${PAYLOAD#v2:}" | base64 -d)
 *   SALT=$(echo "$RAW" | head -c 16 | xxd -p)
 *   IV=$(echo "$RAW"   | tail -c +17 | head -c 12 | xxd -p)
 *   CT=$(echo "$RAW"   | tail -c +29 | base64)
 *   # Derive key with same PBKDF2 parameters:
 *   KEY=$(openssl kdf -keylen 32 -kdfopt digest:SHA256 \
 *         -kdfopt pass:"$PASS" -kdfopt hexsalt:"$SALT" \
 *         -kdfopt iter:200000 PBKDF2 | tr -d ':' )
 *   echo "$CT" | base64 -d | \
 *     openssl enc -d -aes-256-gcm -nosalt -nopad \
 *       -K "$KEY" -iv "$IV"
 *
 * Note: older OpenSSL versions (< 3) do not support AES-GCM via `enc`.
 * In that case use the Python one-liner instead:
 *
 *   python3 - <<'EOF'
 *   import base64, hashlib, sys
 *   from cryptography.hazmat.primitives.ciphers.aead import AESGCM
 *   from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC
 *   from cryptography.hazmat.primitives import hashes
 *   payload = base64.b64decode(input("Paste (without v2: prefix): "))
 *   password = input("Password: ").encode()
 *   salt, iv, ct = payload[:16], payload[16:28], payload[28:]
 *   kdf = PBKDF2HMAC(hashes.SHA256(), 32, salt, 200000)
 *   key = kdf.derive(password)
 *   print(AESGCM(key).decrypt(iv, ct, None).decode())
 *   EOF
 */

"use strict";

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Replace (or append) a query-string parameter in a URL string. */
function replaceUrlParam(url, param, value) {
  if (value == null) value = "";
  var pattern = new RegExp("\\b(" + param + "=).*?(&|#|$)");
  if (pattern.test(url)) return url.replace(pattern, "$1" + value + "$2");
  url = url.replace(/[?#]$/, "");
  return url + (url.indexOf("?") > 0 ? "&" : "?") + param + "=" + value;
}

/** Base URL for flash redirects — always the prefix root. */
function flashBase() {
  return typeof uri_prefix !== "undefined" && uri_prefix !== ""
    ? uri_prefix + "/"
    : "/";
}

// ── AES-256-GCM + PBKDF2-SHA-256 ─────────────────────────────────────────────

var PBKDF2_ITERATIONS = 200000;
var FORMAT_PREFIX = "v2:";

/**
 * Derive a 256-bit AES-GCM key from a password + salt using PBKDF2-SHA-256.
 * @param {string} password
 * @param {Uint8Array} salt
 * @returns {Promise<CryptoKey>}
 */
async function deriveKey(password, salt) {
  var enc = new TextEncoder();
  var keyMaterial = await crypto.subtle.importKey(
    "raw",
    enc.encode(password),
    { name: "PBKDF2" },
    false,
    ["deriveKey"],
  );
  return crypto.subtle.deriveKey(
    {
      name: "PBKDF2",
      salt: salt,
      iterations: PBKDF2_ITERATIONS,
      hash: "SHA-256",
    },
    keyMaterial,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

/**
 * Encrypt plaintext with AES-256-GCM + PBKDF2-SHA-256.
 * @param {string} plaintext
 * @param {string} password
 * @returns {Promise<string>}  "v2:<base64>"
 */
async function aesEncrypt(plaintext, password) {
  var salt = crypto.getRandomValues(new Uint8Array(16));
  var iv = crypto.getRandomValues(new Uint8Array(12));
  var key = await deriveKey(password, salt);
  var enc = new TextEncoder();

  var cipherBuf = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: iv },
    key,
    enc.encode(plaintext),
  );

  // Concatenate: salt(16) + iv(12) + ciphertext+tag
  var ct = new Uint8Array(cipherBuf);
  var out = new Uint8Array(salt.length + iv.length + ct.length);
  out.set(salt, 0);
  out.set(iv, salt.length);
  out.set(ct, salt.length + iv.length);

  // Uint8Array → Base64
  var binary = "";
  // for (var i = 0; i < out.length; i++) binary += String.fromCharCode(out[i]);
  // Try to increase BASE64 performance
  var binary = String.fromCharCode.apply(null, out);
  return FORMAT_PREFIX + btoa(binary);
}

/**
 * Decrypt a "v2:<base64>" ciphertext.
 * @param {string} cipherText
 * @param {string} password
 * @returns {Promise<string|null>}  plaintext or null on failure
 */
async function aesDecrypt(cipherText, password) {
  try {
    var input = cipherText.trim();
    if (!input.startsWith(FORMAT_PREFIX)) return null;

    var binary = atob(input.slice(FORMAT_PREFIX.length));
    var raw = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i++) raw[i] = binary.charCodeAt(i);

    var salt = raw.slice(0, 16);
    var iv = raw.slice(16, 28);
    var cipherBuf = raw.slice(28);

    var key = await deriveKey(password, salt);
    var plainBuf = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: iv },
      key,
      cipherBuf,
    );
    return new TextDecoder().decode(plainBuf);
  } catch (e) {
    return null;
  }
}

// ── Get defaults ───────────────────────────────────────────────────────────────

function getMeta(name) {
  return document.querySelector(`meta[name="${name}"]`)?.content;
}

const default_expiry = getMeta("default-expiry") || "86400";
const default_burn = getMeta("default-burn") || "false";
const uri_prefix = getMeta("uri-prefix") || "";

// ── Render plugin init as JSON ──────────────────────────────────────────────────

function init_plugins() {
  const el = document.getElementById("plugin-inits");
  if (!el) return;

  let inits = [];
  try {
    inits = JSON.parse(el.textContent);
  } catch (e) {
    console.error("Invalid plugin init JSON", e);
    return;
  }

  for (const fnName of inits) {
    if (typeof window[fnName] === "function") {
      window[fnName]();
    }
  }
}

// ── Mobile menu ───────────────────────────────────────────────────────────────

function toggleMobileMenu() {
  //  var menu = document.getElementById("mobileMenu");
  var menu = document.getElementById("desktopNav");
  if (!menu) return;
  if (menu.classList.contains("w3-hide")) {
    menu.classList.remove("w3-hide");
    menu.classList.add("w3-show");
  } else {
    menu.classList.remove("w3-show");
    menu.classList.add("w3-hide");
  }
}

// ── DOM ready ─────────────────────────────────────────────────────────────────

document.addEventListener("DOMContentLoaded", function () {
  // var state = { expiry: "86400", burn: "false" };
  var state = {
    expiry: default_expiry,
    burn: default_burn,
  };

  // ── Apply default labels ─────────────────────────────
  (function () {
    var expiryBtn = document.getElementById("expiry-dropdown-btn");
    var burnBtn = document.getElementById("burn-dropdown-btn");

    if (expiryBtn) {
      var expiryMap = {
        0: "Never",
        300: "5 min",
        600: "10 min",
        3600: "1 hour",
        86400: "1 day",
        604800: "1 week",
        2592000: "1 month",
        31536000: "1 year",
      };
      expiryBtn.textContent =
        "Expires: " + (expiryMap[state.expiry] || state.expiry);
    }

    if (burnBtn) {
      burnBtn.textContent = "Burn: " + (state.burn === "true" ? "Yes" : "No");
    }
  })();

  // ── Language selector → re-highlight ──────────────────────────────────────
  ["language-selector"].forEach(function (id) {
    var sel = document.getElementById(id);
    if (!sel) return;
    sel.addEventListener("change", function () {
      var block = document.getElementById("pastebin-code-block");
      if (block) {
        block.className = "language-" + sel.value;
        if (typeof init_plugins === "function") init_plugins();
      }
    });
  });

  // ── Expiry dropdowns ───────────────────────────────────────────────────────
  ["expiry-dropdown"].forEach(function (ddId) {
    var dd = document.getElementById(ddId);
    if (!dd) return;
    dd.addEventListener("click", function (e) {
      var a = e.target.closest ? e.target.closest("a[href]") : e.target;
      if (!a || a.tagName !== "A") return;
      e.preventDefault();
      state.expiry = a.getAttribute("href");
      var label = a.textContent.trim();
      ["expiry-dropdown-btn"].forEach(function (bId) {
        var b = document.getElementById(bId);
        if (b) b.textContent = "Expires: " + label;
      });
    });
  });

  // ── Burn dropdowns ─────────────────────────────────────────────────────────
  ["burn-dropdown"].forEach(function (ddId) {
    var dd = document.getElementById(ddId);
    if (!dd) return;
    dd.addEventListener("click", function (e) {
      var a = e.target.closest ? e.target.closest("a[href]") : e.target;
      if (!a || a.tagName !== "A") return;
      e.preventDefault();
      state.burn = a.getAttribute("href");
      var label = a.textContent.trim();
      ["burn-dropdown-btn"].forEach(function (bId) {
        var b = document.getElementById(bId);
        if (b) b.textContent = "Burn: " + label;
      });
    });
  });

  // ── Remove button → show deletion modal ───────────────────────────────────
  ["remove-btn"].forEach(function (id) {
    var btn = document.getElementById(id);
    if (!btn) return;
    btn.addEventListener("click", function (e) {
      e.preventDefault();
      var modal = document.getElementById("deletion-modal");
      if (modal) modal.style.display = "block";
    });
  });

  // ── Delete confirmation ────────────────────────────────────────────────────
  // Guard flag prevents double-fire if the button is clicked more than once
  // before the fetch resolves (network lag, double-click, etc.)
  var deleteInFlight = false;
  var deleteConfirmBtn = document.getElementById("deletion-confirm-btn");
  if (deleteConfirmBtn) {
    deleteConfirmBtn.addEventListener("click", function (e) {
      e.preventDefault();
      if (deleteInFlight) return;
      deleteInFlight = true;
      deleteConfirmBtn.disabled = true;

      fetch(window.location.pathname, { method: "DELETE" })
        .then(function (r) {
          return r.json();
        })
        .then(function () {
          var uri = flashBase();
          uri = replaceUrlParam(uri, "level", "info");
          uri = replaceUrlParam(uri, "glyph", "fas fa-info-circle");
          uri = replaceUrlParam(
            uri,
            "msg",
            "The paste has been successfully removed.",
          );
          window.location.href = encodeURI(uri);
        })
        .catch(function () {
          deleteInFlight = false;
          deleteConfirmBtn.disabled = false;
          var uri = flashBase();
          uri = replaceUrlParam(uri, "level", "danger");
          uri = replaceUrlParam(uri, "glyph", "fas fa-circle-xmark");
          uri = replaceUrlParam(uri, "msg", "Failed to delete the paste.");
          window.location.href = encodeURI(uri);
        });
    });
  }

  // ── Copy button ───────────────────────────────────────────────────────────
  ["copy-btn"].forEach(function (id) {
    var btn = document.getElementById(id);
    if (!btn) return;
    btn.addEventListener("click", function (e) {
      e.preventDefault();
      // Try Prism toolbar button first (clipboard.js plugin)
      var toolbarBtn = document.querySelector(".toolbar-item button");
      if (toolbarBtn) {
        toolbarBtn.click();
      } else {
        var block = document.getElementById("pastebin-code-block");
        var text = block ? block.textContent : "";
        if (!text) {
          var ta = document.getElementById("content-textarea");
          text = ta ? ta.value : "";
        }
        if (navigator.clipboard) {
          navigator.clipboard.writeText(text).catch(function () {});
        } else {
          var tmp = document.createElement("textarea");
          tmp.value = text;
          tmp.style.cssText = "position:fixed;opacity:0";
          document.body.appendChild(tmp);
          tmp.focus();
          tmp.select();
          try {
            document.execCommand("copy");
          } catch (err) {}
          document.body.removeChild(tmp);
        }
      }
      var original = btn.textContent;
      btn.textContent = "Copied!";
      btn.disabled = true;
      setTimeout(function () {
        btn.textContent = original;
        btn.disabled = false;
      }, 800);
    });
  });

  // ── Send (create paste) ───────────────────────────────────────────────────
  // Single in-flight flag shared across both desktop and mobile send buttons.
  // Prevents a double-POST if both buttons exist in the DOM simultaneously
  // or if the user clicks quickly before the page navigates away.
  var sendInFlight = false;

  function handleSend(e) {
    e.preventDefault();
    if (sendInFlight) return;
    sendInFlight = true;

    // Disable both buttons for the duration of the request
    ["send-btn"].forEach(function (id) {
      var b = document.getElementById(id);
      if (b) b.disabled = true;
    });

    var langSel = document.getElementById("language-selector");
    var passSel = document.getElementById("pastebin-password");
    var textarea = document.getElementById("content-textarea");

    var uri =
      typeof uri_prefix !== "undefined" && uri_prefix !== ""
        ? uri_prefix + "/"
        : "/";
    if (langSel) uri = replaceUrlParam(uri, "lang", langSel.value);
    uri = replaceUrlParam(uri, "ttl", state.expiry);
    uri = replaceUrlParam(uri, "burn", state.burn);

    var data = textarea ? textarea.value : "";
    var pass = passSel ? passSel.value : "";

    // Map HTTP status codes to user-readable messages.
    // The server sends plain-text error bodies for 4xx/5xx; we never try to
    // JSON-parse them because r.json() would throw and lose the status code.
    function errorMsgForStatus(status) {
      switch (status) {
        case 400: return "Empty paste — nothing to save.";
        case 413: return "Paste is too large. Please reduce the content size and try again.";
        case 429: return "Too many requests. Please wait a moment and try again.";
        case 503: return "Server is busy. Please try again in a few seconds.";
        default:  return "Failed to create paste (HTTP " + status + ").";
      }
    }

    function resetSendBtn() {
      sendInFlight = false;
      ["send-btn"].forEach(function (id) {
        var b = document.getElementById(id);
        if (b) b.disabled = false;
      });
    }

    function doSend(body, encrypted) {
      if (encrypted) uri = replaceUrlParam(uri, "encrypted", "true");
      fetch(uri, {
        method: "POST",
        body: body,
        headers: { "Content-Type": "text/plain" },
      })
        .then(function (r) {
          // Handle error responses before attempting JSON parse.
          // On 4xx/5xx the body is plain text, not JSON.
          if (!r.ok) {
            var msg = errorMsgForStatus(r.status);
            resetSendBtn();
            var redirect = flashBase();
            redirect = replaceUrlParam(redirect, "level", "danger");
            redirect = replaceUrlParam(redirect, "glyph", "fas fa-circle-xmark");
            redirect = replaceUrlParam(redirect, "msg", msg);
            window.location.href = encodeURI(redirect);
            return null; // stop the chain
          }
          return r.json();
        })
        .then(function (result) {
          if (!result) return; // error path already redirected
          var redirect = flashBase();
          redirect = replaceUrlParam(redirect, "level", "success");
          redirect = replaceUrlParam(redirect, "glyph", "fas fa-check");
          redirect = replaceUrlParam(
            redirect,
            "msg",
            "The paste has been successfully created:",
          );
          redirect = replaceUrlParam(redirect, "url", result.url);
          window.location.href = encodeURI(redirect);
        })
        .catch(function () {
          // Network error (no response at all — e.g. offline, DNS failure).
          resetSendBtn();
          var redirect = flashBase();
          redirect = replaceUrlParam(redirect, "level", "danger");
          redirect = replaceUrlParam(redirect, "glyph", "fas fa-circle-xmark");
          redirect = replaceUrlParam(redirect, "msg", "Network error — could not reach the server.");
          window.location.href = encodeURI(redirect);
        });
    }

    if (pass.length > 0) {
      aesEncrypt(data, pass).then(function (encrypted) {
        doSend(encrypted, true);
      });
    } else {
      doSend(data, false);
    }
  }

  ["send-btn"].forEach(function (id) {
    var btn = document.getElementById(id);
    if (btn) btn.addEventListener("click", handleSend);
  });

  // ── Decrypt button (password modal) ───────────────────────────────────────
  var decryptBtn = document.getElementById("decrypt-btn");
  if (decryptBtn) {
    decryptBtn.addEventListener("click", function () {
      var passInput = document.getElementById("modal-password");
      var pass = passInput ? passInput.value : "";
      var block = document.getElementById("pastebin-code-block");
      var textarea = document.getElementById("content-textarea");
      var cipherText = block
        ? block.textContent.trim()
        : //                             : (textarea ? textarea.textContent.trim() : "");
          textarea
          ? textarea.value.trim()
          : "";
      var alertEl = document.getElementById("modal-alert");

      aesDecrypt(cipherText, pass).then(function (decrypted) {
        if (!decrypted || decrypted.length === 0) {
          if (alertEl) alertEl.classList.remove("w3-hide");
        } else {
          if (block) {
            block.textContent = decrypted;
            if (typeof init_plugins === "function") init_plugins();
          } else if (textarea) {
            //            textarea.textContent = decrypted;
            textarea.value = decrypted;
          }
          document.getElementById("password-modal").style.display = "none";
          if (alertEl) alertEl.classList.add("w3-hide");
        }
      });
    });
  }

  // Enter key in password modal triggers decrypt
  var modalPassInput = document.getElementById("modal-password");
  if (modalPassInput) {
    modalPassInput.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        var btn = document.getElementById("decrypt-btn");
        if (btn) btn.click();
      }
    });
  }

  // Close modals on outside-click
  ["password-modal", "deletion-modal"].forEach(function (id) {
    var modal = document.getElementById(id);
    if (!modal) return;
    modal.addEventListener("click", function (e) {
      if (e.target === modal) modal.style.display = "none";
    });
  });
});
