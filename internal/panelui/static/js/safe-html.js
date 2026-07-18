const blockedElementNames = new Set([
  "base",
  "embed",
  "foreignobject",
  "iframe",
  "img",
  "link",
  "meta",
  "object",
  "script",
  "source",
  "style",
  "template",
  "track",
  "use",
  "video",
  "audio"
]);

const URL_ATTRIBUTE_NAMES = new Set([
  "action",
  "formaction",
  "href",
  "poster",
  "src",
  "xlink:href"
]);

const allowedURLProtocols = new Set(["http:", "https:", "mailto:", "tel:"]);
const dangerousStylePattern = /(?:expression\s*\(|url\s*\(|@import|behavior\s*:|-moz-binding\s*:)/i;

// renderSafeHTML is the only permitted string-to-DOM boundary in the panel UI.
// Existing renderers still escape text for layout correctness; this final pass
// makes a missed escape non-executable by stripping active content and URLs.
export function renderSafeHTML(containerElement, markup) {
  if (!containerElement) {
    throw new TypeError("renderSafeHTML requires a container element");
  }

  const inertTemplate = document.createElement("template");
  inertTemplate.innerHTML = String(markup ?? "");
  sanitizeFragment(inertTemplate.content);
  containerElement.replaceChildren(inertTemplate.content);
}

function sanitizeFragment(fragment) {
  for (const element of Array.from(fragment.querySelectorAll("*"))) {
    const normalizedElementName = element.localName.toLowerCase();
    if (blockedElementNames.has(normalizedElementName)) {
      element.remove();
      continue;
    }

    for (const attribute of Array.from(element.attributes)) {
      const normalizedAttributeName = attribute.name.toLowerCase();
      if (normalizedAttributeName.startsWith("on") || normalizedAttributeName === "srcdoc") {
        element.removeAttribute(attribute.name);
        continue;
      }
      if (URL_ATTRIBUTE_NAMES.has(normalizedAttributeName) && !isSafeURL(attribute.value)) {
        element.removeAttribute(attribute.name);
        continue;
      }
      if (normalizedAttributeName === "style" && dangerousStylePattern.test(attribute.value)) {
        element.removeAttribute(attribute.name);
      }
    }
  }
}

function isSafeURL(value) {
  const trimmedValue = String(value || "").trim();
  if (!trimmedValue) {
    return true;
  }

  const compactValue = trimmedValue.replace(/[\u0000-\u0020\u007f]+/g, "");
  if (compactValue.startsWith("#") || compactValue.startsWith("/") || compactValue.startsWith("?")) {
    return true;
  }

  try {
    return allowedURLProtocols.has(new URL(trimmedValue, document.baseURI).protocol);
  } catch {
    return false;
  }
}
