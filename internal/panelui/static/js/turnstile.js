const turnstileScriptURL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

let scriptLoadPromise = null;
let activeWidget = null;
let synchronizationGeneration = 0;

export function preloadTurnstileScript() {
  return loadTurnstileScript();
}

export function synchronizeLoginTurnstile({ enabled, siteKey }) {
  synchronizationGeneration += 1;
  const currentGeneration = synchronizationGeneration;
  const widgetContainer = document.querySelector("[data-turnstile-container]");
  const shouldRender = Boolean(enabled && String(siteKey || "").trim() && widgetContainer);

  if (activeWidget && activeWidget.container !== widgetContainer) {
    removeActiveWidget();
  }
  if (!shouldRender || activeWidget?.container === widgetContainer) {
    return;
  }

  const normalizedSiteKey = String(siteKey).trim();
  updateLoginTurnstileState(widgetContainer, {
    state: "loading",
    statusMessage: "正在加载人机验证组件..."
  });
  void loadTurnstileScript()
    .then((turnstileAPI) => {
      const renderIsStale = currentGeneration !== synchronizationGeneration
        || !widgetContainer.isConnected
        || document.querySelector("[data-turnstile-container]") !== widgetContainer;
      if (renderIsStale) {
        return;
      }

      updateLoginTurnstileState(widgetContainer, {
        state: "ready",
        statusMessage: "请完成人机验证后登录。"
      });
      const widgetIdentifier = turnstileAPI.render(widgetContainer, {
        sitekey: normalizedSiteKey,
        action: "login",
        theme: "light",
        size: "flexible",
        callback(token) {
          const normalizedToken = String(token || "");
          updateLoginTurnstileState(widgetContainer, {
            state: normalizedToken ? "verified" : "error",
            statusMessage: normalizedToken
              ? "人机验证已完成。"
              : "人机验证未返回有效结果，请重新加载。",
            token: normalizedToken
          });
        },
        "expired-callback"() {
          updateLoginTurnstileState(widgetContainer, {
            state: "ready",
            statusMessage: "验证已过期，请重新完成验证。"
          });
        },
        "error-callback"() {
          updateLoginTurnstileState(widgetContainer, {
            state: "error",
            statusMessage: "验证组件暂时不可用，请重新加载。"
          });
        },
        "timeout-callback"() {
          updateLoginTurnstileState(widgetContainer, {
            state: "error",
            statusMessage: "人机验证等待超时，请重新加载。"
          });
        },
        "unsupported-callback"() {
          updateLoginTurnstileState(widgetContainer, {
            state: "error",
            statusMessage: "当前浏览器不支持人机验证组件。"
          });
        }
      });
      activeWidget = {
        container: widgetContainer,
        identifier: widgetIdentifier
      };
    })
    .catch(() => {
      if (currentGeneration !== synchronizationGeneration || !widgetContainer.isConnected) {
        return;
      }
      updateLoginTurnstileState(widgetContainer, {
        state: "error",
        statusMessage: "无法加载人机验证组件，请检查网络后重试。"
      });
    });
}

export function retryLoginTurnstile({ enabled, siteKey }) {
  removeActiveWidget();
  synchronizeLoginTurnstile({ enabled, siteKey });
}

function loadTurnstileScript() {
  if (window.turnstile) {
    return Promise.resolve(window.turnstile);
  }
  if (scriptLoadPromise) {
    return scriptLoadPromise;
  }

  scriptLoadPromise = new Promise((resolve, reject) => {
    const existingScript = document.querySelector('script[data-turnstile-script="true"]');
    const scriptElement = existingScript || document.createElement("script");
    const handleLoad = () => {
      if (!window.turnstile) {
        scriptElement.remove();
        scriptLoadPromise = null;
        reject(new Error("Turnstile API was not initialized."));
        return;
      }
      resolve(window.turnstile);
    };
    const handleError = () => {
      scriptElement.remove();
      scriptLoadPromise = null;
      reject(new Error("Turnstile script failed to load."));
    };

    scriptElement.addEventListener("load", handleLoad, { once: true });
    scriptElement.addEventListener("error", handleError, { once: true });
    if (!existingScript) {
      scriptElement.src = turnstileScriptURL;
      scriptElement.async = true;
      scriptElement.defer = true;
      scriptElement.dataset.turnstileScript = "true";
      document.head.append(scriptElement);
    }
  });
  return scriptLoadPromise;
}

function removeActiveWidget() {
  if (!activeWidget) {
    return;
  }
  try {
    window.turnstile?.remove(activeWidget.identifier);
  } catch {
    // The previous form may already have removed the widget iframe.
  }
  activeWidget = null;
}

function updateLoginTurnstileState(widgetContainer, { state, statusMessage, token = "" }) {
  const loginForm = widgetContainer.closest('form[data-form="login"]');
  const tokenInput = loginForm?.elements?.turnstile_token;
  const statusElement = loginForm?.querySelector("[data-turnstile-status]");
  const submitButton = loginForm?.querySelector('button[type="submit"]');
  const retryButton = loginForm?.querySelector('[data-action="retry-turnstile"]');

  widgetContainer.dataset.turnstileState = state;
  widgetContainer.setAttribute("aria-busy", String(state === "loading"));
  if (tokenInput) {
    tokenInput.value = token;
  }
  if (statusElement) {
    statusElement.textContent = statusMessage;
  }
  if (submitButton) {
    submitButton.disabled = state !== "verified";
  }
  if (retryButton) {
    retryButton.hidden = state !== "error";
  }
}
