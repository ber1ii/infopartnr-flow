(function () {
  'use strict';

  const PARAM_KEY = 'trakyo_id';
  const COOKIE_NAME = 'trakyo_id';
  const STORAGE_KEY = 'trakyo_id';
  const EXPIRY_DAYS = 30;

  // Calendly reliably forwards recognized UTM params into the invitee.created
  // webhook payload's `tracking` object, but does NOT guarantee passthrough of
  // arbitrary custom query params like `trakyo_id`. We mirror the id into a
  // UTM slot as a safety net so it survives into the webhook even if Calendly
  // drops the raw trakyo_id param. utm_content is usually free for this.
  const CALENDLY_UTM_FALLBACK_KEY = 'utm_content';

  // ---------------------------------------------------------------------
  // 1. Extract tracking ID from URL parameters
  // ---------------------------------------------------------------------
  function getQueryParam(key) {
    const urlParams = new URLSearchParams(window.location.search);
    return urlParams.get(key);
  }

  // ---------------------------------------------------------------------
  // 2. Cookie & LocalStorage persistence handlers
  // ---------------------------------------------------------------------
  function setCookie(name, value, days) {
    const date = new Date();
    date.setTime(date.getTime() + days * 24 * 60 * 60 * 1000);
    const expires = '; expires=' + date.toUTCString();
    const secure = window.location.protocol === 'https:' ? '; Secure' : '';
    document.cookie = `${name}=${value || ''}${expires}; path=/; SameSite=Lax${secure}`;
  }

  function getCookie(name) {
    const nameEQ = name + '=';
    const ca = document.cookie.split(';');
    for (let i = 0; i < ca.length; i++) {
      let c = ca[i].trim();
      if (c.indexOf(nameEQ) === 0) return c.substring(nameEQ.length, c.length);
    }
    return null;
  }

  function saveTrakyoID(id) {
    if (!id) return;
    setCookie(COOKIE_NAME, id, EXPIRY_DAYS);
    try {
      localStorage.setItem(STORAGE_KEY, id);
    } catch (e) {}
  }

  function getStoredTrakyoID() {
    return getCookie(COOKIE_NAME) || localStorage.getItem(STORAGE_KEY);
  }

  // ---------------------------------------------------------------------
  // 3. Per-element decoration
  // ---------------------------------------------------------------------

  function decorateStripeLink(link, trakyoID) {
    if (link.dataset.trakyoDecorated) return;
    try {
      const url = new URL(link.href);
      // buy.stripe.com Payment Links support client_reference_id as a query
      // param directly. checkout.stripe.com Checkout Session URLs do NOT —
      // that value has to be set server-side when the Session is created.
      // We still tag the element so app code can read it via window.trakyo
      // and pass it to the backend when creating the session (see API below).
      if (/(^|\.)buy\.stripe\.com$/.test(url.hostname)) {
        url.searchParams.set('client_reference_id', trakyoID);
        link.href = url.toString();
      } else if (/checkout\.stripe\.com$/.test(url.hostname)) {
        link.dataset.trakyoId = trakyoID;
        link.dataset.trakyoNote = 'checkout-session-needs-server-side-client_reference_id';
      }
      link.dataset.trakyoDecorated = '1';
    } catch (e) {}
  }

  function decorateCalendlyElement(elem, trakyoID) {
    if (elem.dataset.trakyoDecorated) return;
    const targetAttr = elem.tagName === 'IFRAME' ? 'src' : 'href';
    try {
      const url = new URL(elem[targetAttr]);
      url.searchParams.set(PARAM_KEY, trakyoID);
      // Safety net: also set a UTM param Calendly is known to forward, so the
      // id survives into invitee.created even if the raw trakyo_id param
      // isn't preserved by Calendly's own link/embed handling.
      if (!url.searchParams.has(CALENDLY_UTM_FALLBACK_KEY)) {
        url.searchParams.set(CALENDLY_UTM_FALLBACK_KEY, trakyoID);
      }
      elem[targetAttr] = url.toString();
      elem.dataset.trakyoDecorated = '1';
    } catch (e) {}
  }

  function decorateForm(form, trakyoID) {
    if (!form.querySelector(`input[name="${PARAM_KEY}"]`)) {
      const hiddenInput = document.createElement('input');
      hiddenInput.type = 'hidden';
      hiddenInput.name = PARAM_KEY;
      hiddenInput.value = trakyoID;
      form.appendChild(hiddenInput);
    } else {
      // Keep an existing hidden input in sync if the id changes mid-session.
      form.querySelector(`input[name="${PARAM_KEY}"]`).value = trakyoID;
    }
  }

  // Scan a single root (document, or a newly-added subtree) and decorate
  // whatever matches inside it. Safe to call repeatedly / idempotently.
  function decorateRoot(root, trakyoID) {
    if (!trakyoID || !root || typeof root.querySelectorAll !== 'function') return;

    root.querySelectorAll('a[href*="buy.stripe.com"], a[href*="checkout.stripe.com"]')
      .forEach((link) => decorateStripeLink(link, trakyoID));

    root.querySelectorAll('a[href*="calendly.com"], iframe[src*="calendly.com"]')
      .forEach((elem) => decorateCalendlyElement(elem, trakyoID));

    root.querySelectorAll('form')
      .forEach((form) => decorateForm(form, trakyoID));
  }

  function decorateOutbound() {
    const trakyoID = getStoredTrakyoID();
    decorateRoot(document, trakyoID);
  }

  // ---------------------------------------------------------------------
  // 4. Watch for dynamically-injected content (Calendly widgets, React
  //    forms, popup builders, etc. commonly render after initial load).
  // ---------------------------------------------------------------------
  let observer = null;
  function startObserving() {
    if (observer || !window.MutationObserver) return;
    let pending = false;

    observer = new MutationObserver((mutations) => {
      if (pending) return;
      const hasElementNodes = mutations.some((m) =>
        Array.from(m.addedNodes || []).some((n) => n.nodeType === 1)
      );
      if (!hasElementNodes) return;

      pending = true;
      // Debounce: batch bursts of DOM changes (e.g. a widget rendering many
      // nodes at once) into a single decoration pass on the next frame.
      requestAnimationFrame(() => {
        pending = false;
        const trakyoID = getStoredTrakyoID();
        if (trakyoID) decorateRoot(document, trakyoID);
      });
    });

    observer.observe(document.body || document.documentElement, {
      childList: true,
      subtree: true,
    });
  }

  // ---------------------------------------------------------------------
  // 5. Public API for JS-driven flows the DOM scan can't reach — e.g. a
  //    React app submitting via fetch() instead of a native form post, or
  //    backend code creating a Stripe Checkout Session and needing the id
  //    to set client_reference_id / metadata server-side.
  // ---------------------------------------------------------------------
  window.trakyo = {
    getId: getStoredTrakyoID,
    // Append trakyo_id (and, for Calendly links, the UTM fallback) to an
    // arbitrary URL string — useful for links built dynamically in app code.
    decorateUrl(href) {
      const trakyoID = getStoredTrakyoID();
      if (!trakyoID) return href;
      try {
        const url = new URL(href);
        url.searchParams.set(PARAM_KEY, trakyoID);
        if (/calendly\.com$/.test(url.hostname) || /calendly\.com$/.test(url.hostname.replace(/^www\./, ''))) {
          url.searchParams.set(CALENDLY_UTM_FALLBACK_KEY, trakyoID);
        }
        return url.toString();
      } catch (e) {
        return href;
      }
    },
    // Convenience for fetch()-based form submits: spread this into your
    // JSON/body payload so the id rides along even without a hidden input.
    getFieldPayload() {
      const trakyoID = getStoredTrakyoID();
      return trakyoID ? { [PARAM_KEY]: trakyoID } : {};
    },
  };

  // ---------------------------------------------------------------------
  // Execution flow
  // ---------------------------------------------------------------------
  const currentUrlID = getQueryParam(PARAM_KEY);
  if (currentUrlID) {
    saveTrakyoID(currentUrlID);
  }

  function init() {
    decorateOutbound();
    startObserving();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();