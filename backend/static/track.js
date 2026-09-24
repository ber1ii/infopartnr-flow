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

  // Backend endpoint for /collect (POST), derived from this script's own
  // origin so it always matches wherever track.js itself is served from.
  const COLLECT_ENDPOINT = (function () {
    var el = document.currentScript;
    if (!el) {
      var scripts = document.getElementsByTagName('script');
      el = scripts[scripts.length - 1];
    }
    try {
      return new URL(el.src).origin + '/collect';
    } catch (e) {
      return '/collect';
    }
  })();

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

  // Typeform's standalone/full-page links accept hidden field values as
  // plain URL query params matching the field's ref (so ?trakyo_id=xxx
  // works directly), but its embed script (embed.typeform.com/next/embed.js)
  // reads hidden field values from a data-tf-hidden="key=value,..."
  // attribute instead, ignoring the href/src for that purpose. We keep
  // both in sync so it works either way the client embedded the form.
  function decorateTypeformElement(elem, trakyoID) {
    if (elem.dataset.trakyoDecorated) return;
    try {
      const isEmbed =
        elem.hasAttribute('data-tf-live') ||
        elem.hasAttribute('data-tf-popup') ||
        elem.hasAttribute('data-tf-slider') ||
        elem.hasAttribute('data-tf-sidetab') ||
        elem.hasAttribute('data-tf-hidden');

      if (isEmbed) {
        const existing = (elem.getAttribute('data-tf-hidden') || '')
          .split(',')
          .map((p) => p.trim())
          .filter((p) => p && !p.startsWith(PARAM_KEY + '='));
        existing.push(`${PARAM_KEY}=${trakyoID}`);
        elem.setAttribute('data-tf-hidden', existing.join(','));
      }

      const targetAttr = elem.tagName === 'IFRAME' ? 'src' : 'href';
      if (elem[targetAttr]) {
        const url = new URL(elem[targetAttr]);
        url.searchParams.set(PARAM_KEY, trakyoID);
        elem[targetAttr] = url.toString();
      }
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

    root.querySelectorAll(
      'a[href*="typeform.com"], iframe[src*="typeform.com"], [data-tf-live], [data-tf-popup], [data-tf-slider], [data-tf-sidetab]'
    ).forEach((elem) => decorateTypeformElement(elem, trakyoID));

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
  // 4b. /collect — links an email the visitor typed somewhere on the site
  //    to their trakyo_id, so a purchase/booking/lead made later under a
  //    different trakyo_id (new device, cleared cookies) can still be
  //    matched back to this visitor by email. Fire-and-forget: this must
  //    never block or fail visibly to the page.
  // ---------------------------------------------------------------------
  function sendCollect(email) {
    const trakyoID = getStoredTrakyoID();
    if (!trakyoID || !email) return;
    const payload = JSON.stringify({ trakyo_id: trakyoID, email: String(email).trim() });
    try {
      if (navigator.sendBeacon) {
        navigator.sendBeacon(COLLECT_ENDPOINT, new Blob([payload], { type: 'text/plain' }));
        return;
      }
    } catch (e) {}
    try {
      fetch(COLLECT_ENDPOINT, {
        method: 'POST',
        body: payload,
        headers: { 'Content-Type': 'text/plain' },
        keepalive: true,
      }).catch(() => {});
    } catch (e) {}
  }

  // Best-effort auto-hook: catches native form submits on the site itself
  // (e.g. a newsletter or lead-capture form) that include an email field.
  // Typeform submissions happen on Typeform's own domain and aren't native
  // form posts here, so they're unaffected by this — their email comes
  // from the answer data in the webhook payload instead.
  function onNativeSubmit(e) {
    const form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    const emailInput = form.querySelector('input[type="email"], input[name*="email" i]');
    if (emailInput && emailInput.value) sendCollect(emailInput.value);
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
    // Call this directly whenever app code captures an email some other
    // way (e.g. a React form submitted via fetch(), or a post-checkout
    // confirmation screen) so it isn't missed by the native-submit hook.
    collect: sendCollect,
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
    document.addEventListener('submit', onNativeSubmit, true);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();