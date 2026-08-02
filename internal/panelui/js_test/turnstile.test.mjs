import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function importStandaloneModule(relativeModulePath) {
  const moduleSource = await readFile(new URL(relativeModulePath, import.meta.url), "utf8");
  const encodedModuleSource = Buffer.from(moduleSource).toString("base64");
  return import(`data:text/javascript;base64,${encodedModuleSource}`);
}

function createLoginWidgetContainer() {
  const tokenInput = { value: "" };
  const statusElement = { textContent: "" };
  const submitButton = { disabled: true };
  const retryButton = { hidden: true };
  const loginForm = {
    elements: { turnstile_token: tokenInput },
    querySelector(selector) {
      if (selector === "[data-turnstile-status]") {
        return statusElement;
      }
      if (selector === 'button[type="submit"]') {
        return submitButton;
      }
      if (selector === '[data-action="retry-turnstile"]') {
        return retryButton;
      }
      return null;
    }
  };
  const widgetContainer = {
    dataset: {},
    isConnected: true,
    attributes: {},
    closest(selector) {
      return selector === 'form[data-form="login"]' ? loginForm : null;
    },
    setAttribute(name, value) {
      this.attributes[name] = String(value);
    }
  };
  return { widgetContainer, tokenInput, statusElement, submitButton, retryButton };
}

test("Turnstile synchronization ignores stale renders and cleans up widgets", async () => {
  let currentWidgetContainer = null;
  const renderCalls = [];
  const removedWidgetIdentifiers = [];

  globalThis.window = {
    turnstile: {
      render(widgetContainer, options) {
        const widgetIdentifier = `widget-${renderCalls.length + 1}`;
        renderCalls.push({ widgetContainer, options, widgetIdentifier });
        return widgetIdentifier;
      },
      remove(widgetIdentifier) {
        removedWidgetIdentifiers.push(widgetIdentifier);
      }
    }
  };
  globalThis.document = {
    querySelector(selector) {
      if (selector === "[data-turnstile-container]") {
        return currentWidgetContainer;
      }
      return null;
    }
  };

  const { retryLoginTurnstile, synchronizeLoginTurnstile } = await importStandaloneModule(
    "../static/js/turnstile.js"
  );
  const staleWidget = createLoginWidgetContainer();
  const currentWidget = createLoginWidgetContainer();

  currentWidgetContainer = staleWidget.widgetContainer;
  synchronizeLoginTurnstile({ enabled: true, siteKey: "stale-site-key" });
  currentWidgetContainer = currentWidget.widgetContainer;
  synchronizeLoginTurnstile({ enabled: true, siteKey: "current-site-key" });
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(renderCalls.length, 1);
  assert.equal(renderCalls[0].widgetContainer, currentWidget.widgetContainer);
  assert.equal(renderCalls[0].options.sitekey, "current-site-key");
  assert.equal(renderCalls[0].options.action, "login");
  assert.equal(renderCalls[0].options.size, "flexible");
  assert.equal(currentWidget.widgetContainer.dataset.turnstileState, "ready");
  assert.equal(currentWidget.submitButton.disabled, true);
  assert.equal(currentWidget.retryButton.hidden, true);

  renderCalls[0].options.callback("browser-issued-token");
  assert.equal(currentWidget.tokenInput.value, "browser-issued-token");
  assert.match(currentWidget.statusElement.textContent, /已完成/);
  assert.equal(currentWidget.submitButton.disabled, false);

  renderCalls[0].options["expired-callback"]();
  assert.equal(currentWidget.tokenInput.value, "");
  assert.match(currentWidget.statusElement.textContent, /已过期/);
  assert.equal(currentWidget.submitButton.disabled, true);

  currentWidget.tokenInput.value = "another-token";
  renderCalls[0].options["error-callback"]();
  assert.equal(currentWidget.tokenInput.value, "");
  assert.match(currentWidget.statusElement.textContent, /暂时不可用/);
  assert.equal(currentWidget.retryButton.hidden, false);

  retryLoginTurnstile({ enabled: true, siteKey: "current-site-key" });
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(removedWidgetIdentifiers, ["widget-1"]);
  assert.equal(renderCalls.length, 2);
  assert.equal(renderCalls[1].widgetContainer, currentWidget.widgetContainer);

  const replacementWidget = createLoginWidgetContainer();
  currentWidgetContainer = replacementWidget.widgetContainer;
  synchronizeLoginTurnstile({ enabled: true, siteKey: "replacement-site-key" });
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(removedWidgetIdentifiers, ["widget-1", "widget-2"]);
  assert.equal(renderCalls.length, 3);
  assert.equal(renderCalls[2].widgetContainer, replacementWidget.widgetContainer);

  currentWidgetContainer = null;
  synchronizeLoginTurnstile({ enabled: false, siteKey: "" });
  assert.deepEqual(removedWidgetIdentifiers, ["widget-1", "widget-2", "widget-3"]);
});
