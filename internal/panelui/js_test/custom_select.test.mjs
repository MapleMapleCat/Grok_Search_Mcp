import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function importStandaloneModule(relativeModulePath) {
  const moduleSource = await readFile(new URL(relativeModulePath, import.meta.url), "utf8");
  const encodedModuleSource = Buffer.from(moduleSource).toString("base64");
  return import(`data:text/javascript;base64,${encodedModuleSource}`);
}

async function importCustomSelectRenderer() {
  const { escapeHTML } = await importStandaloneModule("../static/js/utils.js");
  globalThis.__customSelectTestDependencies = { escapeHTML };

  const componentSource = await readFile(
    new URL("../static/js/components/custom-select.js", import.meta.url),
    "utf8"
  );
  const standaloneSource = componentSource.replace(
    'import { escapeHTML } from "../utils.js";',
    "const { escapeHTML } = globalThis.__customSelectTestDependencies;"
  );
  const encodedModuleSource = Buffer.from(standaloneSource).toString("base64");
  const componentModule = await import(`data:text/javascript;base64,${encodedModuleSource}`);
  delete globalThis.__customSelectTestDependencies;
  return componentModule;
}

const { renderCustomSelect } = await importCustomSelectRenderer();

test("custom select renders synchronized native and visual options", () => {
  const markup = renderCustomSelect({
    id: "tier-select",
    name: "tier_id",
    value: "tier-pro",
    required: true,
    ariaLabel: "配额方案",
    dataAttributes: { action: "change-tier", list: "users" },
    options: [
      { value: "tier-free", label: "免费" },
      { value: "tier-pro", label: "专业版" }
    ]
  });

  assert.match(markup, /class="custom-select"/);
  assert.match(markup, /name="tier_id"/);
  assert.match(markup, /data-action="change-tier"/);
  assert.match(markup, /data-list="users"/);
  assert.match(markup, /<option value="tier-pro" selected/);
  assert.match(markup, /role="combobox"/);
  assert.match(markup, /aria-label="配额方案，当前选择：专业版"/);
  assert.match(markup, /data-value="tier-pro"[^>]*aria-selected="true"|aria-selected="true"[^>]*data-value="tier-pro"/);
});

test("custom select preserves disabled placeholders and form validation", () => {
  const markup = renderCustomSelect({
    id: "stale-tier-select",
    name: "tier_id",
    value: "",
    required: true,
    ariaLabel: "配额方案",
    options: [
      { value: "", label: "当前配额方案不可用", disabled: true },
      { value: "tier-safe", label: "可用方案" }
    ]
  });

  assert.match(markup, /data-custom-select-native[^>]*required/);
  assert.match(markup, /<option value="" selected disabled>当前配额方案不可用<\/option>/);
  assert.match(markup, /data-value="" disabled>当前配额方案不可用<\/button>/);
});

test("custom select escapes labels, values, identifiers, and data attributes", () => {
  const markup = renderCustomSelect({
    id: 'model"select',
    value: 'grok"unsafe',
    ariaLabel: "模型 <选择>",
    compact: true,
    placement: "top",
    dataAttributes: {
      action: 'change"model',
      "invalid_attribute": "ignored"
    },
    options: [{ value: 'grok"unsafe', label: "Grok <unsafe>" }]
  });

  assert.match(markup, /custom-select-compact custom-select-placement-top/);
  assert.match(markup, /id="model&quot;select"/);
  assert.match(markup, /value="grok&quot;unsafe"/);
  assert.match(markup, /Grok &lt;unsafe&gt;/);
  assert.match(markup, /data-action="change&quot;model"/);
  assert.doesNotMatch(markup, /invalid_attribute/);
});

test("all panel selectors render through the shared component", async () => {
  const selectorSourceFiles = [
    "../static/js/components/modal.js",
    "../static/js/components/modal-pagination.js",
    "../static/js/components/pagination.js",
    "../static/js/pages/settings.js"
  ];

  for (const relativeSourcePath of selectorSourceFiles) {
    const source = await readFile(new URL(relativeSourcePath, import.meta.url), "utf8");
    assert.doesNotMatch(source, /<select\b/, `${relativeSourcePath} contains a direct native select`);
  }
});
