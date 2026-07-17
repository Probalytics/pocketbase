import { bodyEditor } from "./richEditor";

window.app = window.app || {};
window.app.modals = window.app.modals || {};

app.modals.openTemplate = function(template, onSaved) {
    const modal = templateModal(template, onSaved);
    document.body.appendChild(modal);
    app.modals.open(modal);
};

function templateModal(template, onSaved) {
    const data = store({
        record: template ? { ...template } : { id: "", name: "", subject: "", body: "" },
        isSaving: false,
    });

    async function save() {
        if (data.isSaving) return;
        data.isSaving = true;
        try {
            const payload = { name: data.record.name, subject: data.record.subject, body: data.record.body };
            if (data.record.id) {
                await app.pb.collection("_mailTemplates").update(data.record.id, payload);
            } else {
                await app.pb.collection("_mailTemplates").create(payload);
            }
            app.toasts.success("Template saved.");
            app.modals.close();
            onSaved?.();
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
    }

    return t.div(
        { className: "modal lg", onafterclose: (el) => el?.remove() },
        t.header(
            { className: "modal-header" },
            t.h5({ className: "m-auto" }, data.record.id ? "Edit template" : "New template"),
        ),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "grid" },
                cell(field("Name", () =>
                    t.input({
                        type: "text",
                        value: () => data.record.name || "",
                        oninput: (e) => (data.record.name = e.target.value),
                    }))),
                cell(field("Subject", () =>
                    t.input({
                        type: "text",
                        value: () => data.record.subject || "",
                        oninput: (e) => (data.record.subject = e.target.value),
                    }))),
                cell(bodyEditor(
                    "Body",
                    () => data.record.body || "",
                    (val) => (data.record.body = val),
                )),
            ),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button(
                { type: "button", className: "btn transparent m-r-auto", onclick: () => app.modals.close() },
                t.span({ className: "txt" }, "Cancel"),
            ),
            t.button(
                { type: "button", className: () => `btn ${data.isSaving ? "loading" : ""}`, onclick: save },
                t.span({ className: "txt" }, data.record.id ? "Save changes" : "Create"),
            ),
        ),
    );
}

function cell(control) {
    return t.div({ className: "col-lg-12" }, control);
}
function field(label, control) {
    return t.div({ className: "field" }, t.label(null, label), control);
}
