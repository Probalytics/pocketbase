import { filterField } from "./filterEditor";

window.app = window.app || {};
window.app.modals = window.app.modals || {};

app.modals.openSegment = function(segment, onSaved) {
    const modal = segmentModal(segment, onSaved);
    document.body.appendChild(modal);
    app.modals.open(modal);
};

function segmentModal(segment, onSaved) {
    const data = store({
        record: segment ? { ...segment } : { id: "", name: "", collection: "", filter: "" },
        isSaving: false,
        isCounting: false,
        membersCount: null,
    });

    const segmentCollections = () =>
        app.store.collections.filter(
            (c) => !c.system && (c.type === "auth" || c.fields.some((f) => f.name === "email")),
        );

    async function countMembers() {
        if (data.isCounting || !data.record.collection) return;
        data.isCounting = true;
        try {
            const res = await app.pb.send("/api/marketing/audience", {
                method: "GET",
                query: { collection: data.record.collection, filter: data.record.filter || "" },
            });
            data.membersCount = res.total;
        } catch (err) {
            data.membersCount = null;
            app.checkApiError(err);
        }
        data.isCounting = false;
    }

    async function save() {
        if (data.isSaving) return;
        data.isSaving = true;
        try {
            const payload = {
                name: data.record.name,
                collection: data.record.collection,
                filter: data.record.filter,
            };
            if (data.record.id) {
                await app.pb.collection("_mailSegments").update(data.record.id, payload);
            } else {
                await app.pb.collection("_mailSegments").create(payload);
            }
            app.toasts.success("Audience saved.");
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
            t.h5({ className: "m-auto" }, data.record.id ? "Edit audience" : "New audience"),
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
                cell(field("Collection", () =>
                    app.components.select({
                        placeholder: "Select collection",
                        options: () => segmentCollections().map((c) => ({ value: c.name, label: c.name })),
                        value: () => data.record.collection || "",
                        onchange: (s) => {
                            data.record.collection = s?.[0]?.value || "";
                            data.membersCount = null;
                        },
                    }))),
                cell(filterField(
                    "Filter",
                    () => data.record.collection,
                    () => data.record.filter || "",
                    (val) => (data.record.filter = val),
                    "plan='pro' && verified=true",
                )),
                cell(t.div(
                    { className: "flex gap-10" },
                    t.button(
                        {
                            type: "button",
                            className: () => `btn sm secondary ${data.isCounting ? "loading" : ""}`,
                            onclick: countMembers,
                        },
                        t.i({ className: "ri-group-line" }),
                        t.span({ className: "txt" }, "Count members"),
                    ),
                    () =>
                        data.membersCount !== null
                            ? t.strong({ className: "txt-nowrap" }, `${data.membersCount} members`)
                            : t.span(null, ""),
                    t.button(
                        {
                            type: "button",
                            className: "btn sm secondary transparent m-l-auto",
                            disabled: () => !data.record.collection,
                            onclick: () => {
                                app.modals.close();
                                window.location.hash = audiencePreviewHash(data.record);
                            },
                        },
                        t.i({ className: "ri-eye-line" }),
                        t.span({ className: "txt" }, "Preview members"),
                    ),
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

export function audiencePreviewHash(segment) {
    return "#/crm/contacts?collection=" + encodeURIComponent(segment.collection)
        + "&filter=" + encodeURIComponent(segment.filter || "");
}

function cell(control) {
    return t.div({ className: "col-lg-12" }, control);
}
function field(label, control) {
    return t.div({ className: "field" }, t.label(null, label), control);
}
