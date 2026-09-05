window.app = window.app || {};

// bodyEditor renders PocketBase's native TinyMCE rich editor (the same one
// used by "editor" collection fields) inside a labeled field box. The editor
// is created lazily on mount to keep initial render fast.
export function bodyEditor(label, getValue, setValue, disabled) {
    const local = store({ editor: "" });
    let timer;

    return t.div(
        {
            className: "record-field-input field-type-editor",
            onmount: () => {
                timer = setTimeout(() => {
                    const opts = { value: getValue, onchange: setValue };
                    if (disabled) {
                        opts.disabled = disabled;
                    }
                    local.editor = app.components.tinymce(opts);
                }, 0);
            },
            onunmount: () => clearTimeout(timer),
        },
        t.div(
            { className: "field" },
            t.label(
                null,
                t.i({ className: app.fieldTypes.editor.icon, ariaHidden: true }),
                t.span({ className: "txt" }, label),
            ),
            () => local.editor,
        ),
    );
}
