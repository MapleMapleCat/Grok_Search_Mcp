import { escapeHTML } from "../utils.js";

const typeaheadStateBySelect = new WeakMap();
let customSelectsConfigured = false;

export function renderCustomSelect({
  id,
  name = "",
  value = "",
  options = [],
  required = false,
  disabled = false,
  ariaLabel = "选择选项",
  dataAttributes = {},
  compact = false,
  placement = "bottom"
}) {
  const selectIdentifier = String(id || "").trim();
  if (!selectIdentifier) {
    throw new TypeError("renderCustomSelect requires a stable id");
  }

  const normalizedOptions = options.map((option) => ({
    value: String(option?.value ?? ""),
    label: String(option?.label ?? option?.value ?? ""),
    disabled: Boolean(option?.disabled),
    selected: Boolean(option?.selected)
  }));
  const selectedOptionIndex = findSelectedOptionIndex(normalizedOptions, value);
  const selectedOption = normalizedOptions[selectedOptionIndex] || { value: "", label: "" };
  const nativeSelectIdentifier = `${selectIdentifier}-native`;
  const listboxIdentifier = `${selectIdentifier}-listbox`;
  const wrapperClasses = [
    "custom-select",
    compact ? "custom-select-compact" : "",
    placement === "top" ? "custom-select-placement-top" : ""
  ].filter(Boolean).join(" ");
  const nativeAttributes = renderNativeSelectAttributes({
    id: nativeSelectIdentifier,
    name,
    required,
    disabled,
    dataAttributes
  });

  return `
    <div class="${wrapperClasses}" data-custom-select data-custom-select-label="${escapeHTML(ariaLabel)}">
      <select ${nativeAttributes}>
        ${normalizedOptions.map((option, optionIndex) => renderNativeOption(option, optionIndex === selectedOptionIndex)).join("")}
      </select>
      <button class="custom-select-trigger" id="${escapeHTML(selectIdentifier)}" type="button" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-controls="${escapeHTML(listboxIdentifier)}" aria-label="${escapeHTML(formatTriggerAccessibleLabel(ariaLabel, selectedOption.label))}" data-custom-select-trigger ${disabled ? "disabled" : ""}>
        <span class="custom-select-value" data-custom-select-value>${escapeHTML(selectedOption.label)}</span>
        <span class="custom-select-chevron" aria-hidden="true"></span>
      </button>
      <div class="custom-select-menu" id="${escapeHTML(listboxIdentifier)}" role="listbox" aria-label="${escapeHTML(ariaLabel)}" data-custom-select-menu popover="manual" hidden>
        ${normalizedOptions.map((option, optionIndex) => renderCustomOption({
          option,
          optionIndex,
          selectIdentifier,
          selected: optionIndex === selectedOptionIndex
        })).join("")}
      </div>
    </div>
  `;
}

export function configureCustomSelects(documentElement = document) {
  if (customSelectsConfigured) {
    return;
  }
  customSelectsConfigured = true;

  documentElement.addEventListener("click", handleDocumentClick);
  documentElement.addEventListener("keydown", handleDocumentKeydown);
  documentElement.addEventListener("change", handleNativeSelectChange);
  documentElement.addEventListener("invalid", handleNativeSelectInvalid, true);
  documentElement.addEventListener("scroll", handleDocumentScroll, true);
  window.addEventListener("resize", () => closeAllCustomSelects());
}

function findSelectedOptionIndex(options, selectedValue) {
  const explicitlySelectedIndex = options.findIndex((option) => option.selected);
  if (explicitlySelectedIndex >= 0) {
    return explicitlySelectedIndex;
  }

  const normalizedSelectedValue = String(selectedValue ?? "");
  const matchingValueIndex = options.findIndex((option) => option.value === normalizedSelectedValue);
  return matchingValueIndex >= 0 ? matchingValueIndex : 0;
}

function renderNativeSelectAttributes({ id, name, required, disabled, dataAttributes }) {
  const attributes = [
    `id="${escapeHTML(id)}"`,
    "class=\"custom-select-native\"",
    "data-custom-select-native",
    "aria-hidden=\"true\"",
    "tabindex=\"-1\""
  ];

  if (name) {
    attributes.push(`name="${escapeHTML(name)}"`);
  }
  if (required) {
    attributes.push("required");
  }
  if (disabled) {
    attributes.push("disabled");
  }

  for (const [attributeName, attributeValue] of Object.entries(dataAttributes)) {
    if (!/^[a-z][a-z0-9-]*$/.test(attributeName) || attributeValue === undefined || attributeValue === null) {
      continue;
    }
    attributes.push(`data-${attributeName}="${escapeHTML(attributeValue)}"`);
  }

  return attributes.join(" ");
}

function renderNativeOption(option, selected) {
  return `<option value="${escapeHTML(option.value)}" ${selected ? "selected" : ""} ${option.disabled ? "disabled" : ""}>${escapeHTML(option.label)}</option>`;
}

function renderCustomOption({ option, optionIndex, selectIdentifier, selected }) {
  return `<button class="custom-select-option" id="${escapeHTML(`${selectIdentifier}-option-${optionIndex}`)}" type="button" role="option" aria-selected="${selected ? "true" : "false"}" data-custom-select-option data-value="${escapeHTML(option.value)}" ${option.disabled ? "disabled" : ""}>${escapeHTML(option.label)}</button>`;
}

function formatTriggerAccessibleLabel(ariaLabel, selectedLabel) {
  return selectedLabel ? `${ariaLabel}，当前选择：${selectedLabel}` : ariaLabel;
}

function handleDocumentClick(event) {
  const optionElement = event.target.closest("[data-custom-select-option]");
  if (optionElement) {
    selectCustomOption(optionElement);
    return;
  }

  const triggerElement = event.target.closest("[data-custom-select-trigger]");
  if (triggerElement) {
    const customSelectElement = triggerElement.closest("[data-custom-select]");
    if (customSelectElement?.classList.contains("is-open")) {
      closeCustomSelect(customSelectElement);
    } else if (customSelectElement) {
      openCustomSelect(customSelectElement);
    }
    return;
  }

  closeAllCustomSelects();
}

function handleDocumentKeydown(event) {
  const triggerElement = event.target.closest("[data-custom-select-trigger]");
  if (triggerElement) {
    handleTriggerKeydown(event, triggerElement);
    return;
  }

  const optionElement = event.target.closest("[data-custom-select-option]");
  if (optionElement) {
    handleOptionKeydown(event, optionElement);
  }
}

function handleTriggerKeydown(event, triggerElement) {
  const customSelectElement = triggerElement.closest("[data-custom-select]");
  if (!customSelectElement || triggerElement.disabled) {
    return;
  }

  if (["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) {
    event.preventDefault();
    openCustomSelect(customSelectElement, event.key === "ArrowUp" ? "last" : "selected");
    return;
  }

  if (isTypeaheadKey(event)) {
    event.preventDefault();
    openCustomSelect(customSelectElement, "selected");
    focusTypeaheadMatch(customSelectElement, event.key);
  }
}

function handleOptionKeydown(event, optionElement) {
  const customSelectElement = optionElement.closest("[data-custom-select]");
  if (!customSelectElement) {
    return;
  }

  if (event.key === "Escape") {
    event.preventDefault();
    event.stopImmediatePropagation();
    closeCustomSelect(customSelectElement, true);
    return;
  }
  if (event.key === "Tab") {
    closeCustomSelect(customSelectElement);
    return;
  }
  if (["Enter", " "].includes(event.key)) {
    event.preventDefault();
    selectCustomOption(optionElement);
    return;
  }
  if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    focusAdjacentOption(customSelectElement, optionElement, event.key);
    return;
  }
  if (isTypeaheadKey(event)) {
    event.preventDefault();
    focusTypeaheadMatch(customSelectElement, event.key);
  }
}

function handleNativeSelectChange(event) {
  if (!event.target.matches("[data-custom-select-native]")) {
    return;
  }

  const customSelectElement = event.target.closest("[data-custom-select]");
  if (customSelectElement) {
    synchronizeCustomSelect(customSelectElement);
  }
}

function handleNativeSelectInvalid(event) {
  if (!event.target.matches("[data-custom-select-native]")) {
    return;
  }

  event.preventDefault();
  const customSelectElement = event.target.closest("[data-custom-select]");
  const triggerElement = customSelectElement?.querySelector("[data-custom-select-trigger]");
  customSelectElement?.classList.add("is-invalid");
  triggerElement?.focus();
}

function handleDocumentScroll(event) {
  if (event.target instanceof Element && event.target.closest("[data-custom-select-menu]")) {
    return;
  }
  closeAllCustomSelects();
}

function openCustomSelect(customSelectElement, focusPreference = "selected") {
  const triggerElement = customSelectElement.querySelector("[data-custom-select-trigger]");
  const menuElement = customSelectElement.querySelector("[data-custom-select-menu]");
  if (!triggerElement || !menuElement || triggerElement.disabled) {
    return;
  }

  closeAllCustomSelects(customSelectElement);
  customSelectElement.classList.add("is-open");
  triggerElement.setAttribute("aria-expanded", "true");
  menuElement.hidden = false;
  showAndPositionPopover(customSelectElement, triggerElement, menuElement);

  const availableOptions = getAvailableOptions(customSelectElement);
  const selectedOption = availableOptions.find((option) => option.getAttribute("aria-selected") === "true");
  const optionToFocus = focusPreference === "last"
    ? availableOptions.at(-1)
    : selectedOption || availableOptions[0];
  optionToFocus?.focus();
}

function closeCustomSelect(customSelectElement, restoreTriggerFocus = false) {
  const triggerElement = customSelectElement.querySelector("[data-custom-select-trigger]");
  const menuElement = customSelectElement.querySelector("[data-custom-select-menu]");
  customSelectElement.classList.remove("is-open");
  triggerElement?.setAttribute("aria-expanded", "false");
  if (menuElement) {
    if (typeof menuElement.hidePopover === "function") {
      try {
        menuElement.hidePopover();
      } catch {
        // The fallback menu may not have entered the top layer.
      }
    }
    menuElement.hidden = true;
    menuElement.removeAttribute("style");
  }
  if (restoreTriggerFocus) {
    triggerElement?.focus();
  }
}

function closeAllCustomSelects(excludedSelectElement = null) {
  for (const customSelectElement of document.querySelectorAll("[data-custom-select].is-open")) {
    if (customSelectElement !== excludedSelectElement) {
      closeCustomSelect(customSelectElement);
    }
  }
}

function showAndPositionPopover(customSelectElement, triggerElement, menuElement) {
  if (typeof menuElement.showPopover !== "function") {
    return;
  }

  try {
    menuElement.showPopover();
  } catch {
    return;
  }

  const viewportPadding = 8;
  const maximumMenuHeight = Math.min(280, window.innerHeight * 0.5);
  const triggerRectangle = triggerElement.getBoundingClientRect();
  const spaceAbove = triggerRectangle.top - viewportPadding;
  const spaceBelow = window.innerHeight - triggerRectangle.bottom - viewportPadding;
  const prefersTopPlacement = customSelectElement.classList.contains("custom-select-placement-top");
  const shouldOpenAbove = prefersTopPlacement || (spaceBelow < 180 && spaceAbove > spaceBelow);
  const availableHeight = Math.max(80, shouldOpenAbove ? spaceAbove - 7 : spaceBelow - 7);
  const menuHeightLimit = Math.min(maximumMenuHeight, availableHeight);
  const menuWidth = Math.max(triggerRectangle.width, 82);
  const menuLeft = Math.min(
    Math.max(viewportPadding, triggerRectangle.left),
    window.innerWidth - menuWidth - viewportPadding
  );

  menuElement.style.position = "fixed";
  menuElement.style.right = "auto";
  menuElement.style.bottom = "auto";
  menuElement.style.left = `${menuLeft}px`;
  menuElement.style.width = `${menuWidth}px`;
  menuElement.style.maxHeight = `${menuHeightLimit}px`;

  const measuredMenuHeight = menuElement.getBoundingClientRect().height;
  const menuTop = shouldOpenAbove
    ? Math.max(viewportPadding, triggerRectangle.top - measuredMenuHeight - 7)
    : Math.min(window.innerHeight - measuredMenuHeight - viewportPadding, triggerRectangle.bottom + 7);
  menuElement.style.top = `${menuTop}px`;
}

function selectCustomOption(optionElement) {
  if (optionElement.disabled) {
    return;
  }

  const customSelectElement = optionElement.closest("[data-custom-select]");
  const nativeSelectElement = customSelectElement?.querySelector("[data-custom-select-native]");
  if (!customSelectElement || !nativeSelectElement) {
    return;
  }

  nativeSelectElement.value = optionElement.dataset.value ?? "";
  nativeSelectElement.dispatchEvent(new Event("change", { bubbles: true }));
  closeCustomSelect(customSelectElement, true);
}

function synchronizeCustomSelect(customSelectElement) {
  const nativeSelectElement = customSelectElement.querySelector("[data-custom-select-native]");
  const triggerElement = customSelectElement.querySelector("[data-custom-select-trigger]");
  const valueElement = customSelectElement.querySelector("[data-custom-select-value]");
  if (!nativeSelectElement || !triggerElement || !valueElement) {
    return;
  }

  const selectedValue = nativeSelectElement.value;
  const selectedNativeOption = nativeSelectElement.selectedOptions[0];
  const selectedLabel = selectedNativeOption?.textContent?.trim() || "";
  const ariaLabel = customSelectElement.dataset.customSelectLabel || "选择选项";

  for (const optionElement of customSelectElement.querySelectorAll("[data-custom-select-option]")) {
    optionElement.setAttribute("aria-selected", optionElement.dataset.value === selectedValue ? "true" : "false");
  }
  valueElement.textContent = selectedLabel;
  triggerElement.setAttribute("aria-label", formatTriggerAccessibleLabel(ariaLabel, selectedLabel));
  triggerElement.disabled = nativeSelectElement.disabled;
  customSelectElement.classList.toggle("is-invalid", !nativeSelectElement.checkValidity());
}

function focusAdjacentOption(customSelectElement, currentOptionElement, key) {
  const availableOptions = getAvailableOptions(customSelectElement);
  if (availableOptions.length === 0) {
    return;
  }

  if (key === "Home") {
    availableOptions[0].focus();
    return;
  }
  if (key === "End") {
    availableOptions.at(-1).focus();
    return;
  }

  const currentOptionIndex = availableOptions.indexOf(currentOptionElement);
  const direction = key === "ArrowUp" ? -1 : 1;
  const nextOptionIndex = (currentOptionIndex + direction + availableOptions.length) % availableOptions.length;
  availableOptions[nextOptionIndex].focus();
}

function focusTypeaheadMatch(customSelectElement, typedCharacter) {
  const availableOptions = getAvailableOptions(customSelectElement);
  if (availableOptions.length === 0) {
    return;
  }

  const previousState = typeaheadStateBySelect.get(customSelectElement) || { query: "", timer: null };
  clearTimeout(previousState.timer);
  const normalizedCharacter = typedCharacter.toLocaleLowerCase("zh-CN");
  const query = `${previousState.query}${normalizedCharacter}`;
  const matchingOption = availableOptions.find((option) => option.textContent.trim().toLocaleLowerCase("zh-CN").startsWith(query))
    || availableOptions.find((option) => option.textContent.trim().toLocaleLowerCase("zh-CN").startsWith(normalizedCharacter));
  matchingOption?.focus();

  const timer = setTimeout(() => {
    typeaheadStateBySelect.delete(customSelectElement);
  }, 650);
  typeaheadStateBySelect.set(customSelectElement, { query: matchingOption ? query : normalizedCharacter, timer });
}

function getAvailableOptions(customSelectElement) {
  return Array.from(customSelectElement.querySelectorAll("[data-custom-select-option]:not(:disabled)"));
}

function isTypeaheadKey(event) {
  return event.key.length === 1 && !event.altKey && !event.ctrlKey && !event.metaKey;
}
