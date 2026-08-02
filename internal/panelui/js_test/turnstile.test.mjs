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
  const loginForm = {
    elements: { turnstile_token: tokenInput },
    querySelector(selector) {
      return selector === "[data-turnstile-status]" ? statusElement : null;
    }
  };
  const widgetContainer = {
    isConnected: true,
    closest(selector) {
      return selector === 'form[data-form="login"]' ? loginForm : null;
    }
  };
  return { widgetContainer, tokenInput, statusElement };
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

  const { synchronizeLoginTurnstile } = await importStandaloneModule("../static/js/turnstile.js");
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

  renderCalls[0].options.callback("browser-issued-token");
  assert.equal(currentWidget.tokenInput.value, "browser-issued-token");
  assert.match(currentWidget.statusElement.textContent, /已完成/);

  renderCalls[0].options["expired-callback"]();
  assert.equal(currentWidget.tokenInput.value, "");
  assert.match(currentWidget.statusElement.textContent, /已过期/);

  currentWidget.tokenInput.value = "another-token";
  renderCalls[0].options["error-callback"]();
  assert.equal(currentWidget.tokenInput.value, "");
  assert.match(currentWidget.statusElement.textContent, /暂时不可用/);

  const replacementWidget = createLoginWidgetContainer();
  currentWidgetContainer = replacementWidget.widgetContainer;
  synchronizeLoginTurnstile({ enabled: true, siteKey: "replacement-site-key" });
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(removedWidgetIdentifiers, ["widget-1"]);
  assert.equal(renderCalls.length, 2);
  assert.equal(renderCalls[1].widgetContainer, replacementWidget.widgetContainer);

  currentWidgetContainer = null;
  synchronizeLoginTurnstile({ enabled: false, siteKey: "" });
  assert.deepEqual(removedWidgetIdentifiers, ["widget-1", "widget-2"]);
});
