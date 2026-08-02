const turnstileScriptURL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

let scriptLoadPromise = null;
let activeWidget = null;
let synchronizationGeneration = 0;

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
  void loadTurnstileScript()
    .then((turnstileAPI) => {
      const renderIsStale = currentGeneration !== synchronizationGeneration
        || !widgetContainer.isConnected
        || document.querySelector("[data-turnstile-container]") !== widgetContainer;
      if (renderIsStale) {
        return;
      }

      const loginForm = widgetContainer.closest('form[data-form="login"]');
      const tokenInput = loginForm?.elements?.turnstile_token;
      const statusElement = loginForm?.querySelector("[data-turnstile-status]");
      const widgetIdentifier = turnstileAPI.render(widgetContainer, {
        sitekey: normalizedSiteKey,
        action: "login",
        theme: "light",
        size: "flexible",
        callback(token) {
          if (tokenInput) {
            tokenInput.value = String(token || "");
          }
          if (statusElement) {
            statusElement.textContent = "人机验证已完成。";
          }
        },
        "expired-callback"() {
          if (tokenInput) {
            tokenInput.value = "";
          }
          if (statusElement) {
            statusElement.textContent = "验证已过期，请重新完成验证。";
          }
        },
        "error-callback"() {
          if (tokenInput) {
            tokenInput.value = "";
          }
          if (statusElement) {
            statusElement.textContent = "验证组件暂时不可用，请稍后重试。";
          }
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
      const statusElement = widgetContainer
        .closest('form[data-form="login"]')
        ?.querySelector("[data-turnstile-status]");
      if (statusElement) {
        statusElement.textContent = "无法加载人机验证组件，请检查网络后重试。";
      }
    });
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
