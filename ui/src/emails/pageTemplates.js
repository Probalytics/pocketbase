import { emailsLayout } from "./emailsLayout";

const COLLECTION = "_mailTemplates";

export function pageTemplates() {
    app.store.title = "Templates";

    const data = store({
        isLoading: true,
        templates: [],
        editing: null,
        isSaving: false,
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.templates = await app.pb.collection(COLLECTION).getFullList({ sort: "name" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    function edit(template) {
        data.editing = template ? { ...template } : { id: "", name: "", subject: "", body: "" };
    }

    async function save() {
        if (data.isSaving) return;
        data.isSaving = true;
        try {
            const payload = { name: data.editing.name, subject: data.editing.subject, body: data.editing.body };
            if (data.editing.id) {
                await app.pb.collection(COLLECTION).update(data.editing.id, payload);
            } else {
                await app.pb.collection(COLLECTION).create(payload);
            }
            app.toasts.success("Template saved.");
            data.editing = null;
            await load();
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
    }

    function remove(template) {
        app.modals.confirm(`Delete template "${template.name}"?`, async () => {
            try {
                await app.pb.collection(COLLECTION).delete(template.id);
                await load();
            } catch (err) {
                app.checkApiError(err);
            }
        });
    }

    const newButton = t.button(
        { type: "button", className: "btn expanded", onclick: () => edit(null) },
        t.i({ className: "ri-add-line" }),
        t.span({ className: "txt" }, "New template"),
    );

    return emailsLayout([{ label: "Templates" }], () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
        }
        return t.div(
            { className: "grid" },
            t.div(
                { className: () => (data.editing ? "col-lg-6" : "col-lg-12") },
                data.templates.length
                    ? t.table(
                        { className: "table" },
                        t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Subject"), t.th(null, ""))),
                        t.tbody(
                            null,
                            ...data.templates.map((template) =>
                                t.tr(
                                    { className: "row-handle" },
                                    t.td(null, t.strong(null, template.name)),
                                    t.td({ className: "txt-hint" }, template.subject),
                                    t.td(
                                        { className: "txt-right" },
                                        t.button(
                                            {
                                                type: "button",
                                                className: "btn sm secondary transparent",
                                                onclick: () => edit(template),
                                            },
                                            t.span({ className: "txt" }, "Edit"),
                                        ),
                                        t.button(
                                            {
                                                type: "button",
                                                className: "btn sm danger transparent",
                                                onclick: () => remove(template),
                                            },
                                            t.i({ className: "ri-delete-bin-line" }),
                                        ),
                                    ),
                                )
                            ),
                        ),
                    )
                    : t.p({ className: "txt-hint p-base" }, "No templates yet."),
            ),
            () => (data.editing ? editor() : undefined),
        );
    }, newButton);

    function editor() {
        return t.div(
            { className: "col-lg-6" },
            t.div(
                { className: "email-card" },
                t.div(
                    { className: "grid" },
                    t.div({ className: "col-lg-12 txt-bold" }, data.editing.id ? "Edit template" : "New template"),
                    cell(field("Name", () =>
                        t.input({
                            type: "text",
                            value: () => data.editing.name || "",
                            oninput: (e) => (data.editing.name = e.target.value),
                        }))),
                    cell(field("Subject", () =>
                        t.input({
                            type: "text",
                            value: () => data.editing.subject || "",
                            oninput: (e) => (data.editing.subject = e.target.value),
                        }))),
                    cell(field("Body (HTML)", () =>
                        t.textarea({
                            rows: 12,
                            className: "txt-mono",
                            value: () => data.editing.body || "",
                            oninput: (e) => (data.editing.body = e.target.value),
                        }))),
                    t.div(
                        { className: "col-lg-12 flex gap-10" },
                        t.button(
                            {
                                type: "button",
                                className: "btn secondary transparent",
                                onclick: () => (data.editing = null),
                            },
                            t.span({ className: "txt" }, "Cancel"),
                        ),
                        t.button(
                            {
                                type: "button",
                                className: () => `btn expanded m-l-auto ${data.isSaving ? "loading" : ""}`,
                                onclick: save,
                            },
                            t.span({ className: "txt" }, "Save"),
                        ),
                    ),
                ),
            ),
        );
    }
}

function cell(control) {
    return t.div({ className: "col-lg-12" }, control);
}
function field(label, control) {
    return t.div({ className: "field" }, t.label(null, label), control);
}
