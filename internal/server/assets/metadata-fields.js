(() => {
  "use strict";
  const form = document.querySelector("[data-metadata-form]");
  if (!form) return;
  const list = form.querySelector("[data-field-list]");
  const count = form.querySelector("[data-field-count]");
  const template = document.querySelector("[data-field-template]");

  const reindex = () => {
    const fields = Array.from(list.querySelectorAll("[data-field]"));
    count.value = String(fields.length);
    fields.forEach((field, index) => {
      const number = index + 1;
      const mappings = [
        ["[data-field-code]", "field_code_", "field-code-", "[data-code-label]"],
        ["[data-field-type]", "field_type_", "field-type-", "[data-type-label]"],
        ["[data-field-required]", "field_required_", "field-required-", "[data-required-label]"],
        ["[data-field-read-only]", "field_read_only_", "field-read-only-", "[data-read-only-label]"]
      ];
      field.querySelector("[data-field-heading]").id = "field-heading-" + index;
      field.querySelector("[data-field-heading]").textContent = "Field " + number;
      field.setAttribute("aria-labelledby", "field-heading-" + index);
      mappings.forEach(([inputSelector, namePrefix, idPrefix, labelSelector]) => {
        const input = field.querySelector(inputSelector);
        const id = idPrefix + index;
        input.name = namePrefix + index;
        input.id = id;
        field.querySelector(labelSelector).htmlFor = id;
      });
    });
  };

  form.addEventListener("click", (event) => {
    const add = event.target.closest("[data-add-field]");
    if (add) {
      if (list.querySelectorAll("[data-field]").length >= 200) return;
      list.append(template.content.cloneNode(true));
      reindex();
      list.lastElementChild.querySelector("[data-field-code]").focus();
      return;
    }
    const remove = event.target.closest("[data-remove-field]");
    if (remove) {
      remove.closest("[data-field]").remove();
      reindex();
    }
  });
  form.addEventListener("submit", reindex);
  reindex();
})();
