import { contactAvatar } from "./contactModal";
import { filterField } from "./filterEditor";

window.app = window.app || {};
window.app.modals = window.app.modals || {};

const membersPreviewSize = 8;

app.modals.openSegment = function(segment, onSaved) {
    const modal = segmentModal(segment, onSaved);
    document.body.appendChild(modal);
    app.modals.open(modal);
};

function segmentModal(segment, onSaved) {
    const data = store({
        record: segment ? { ...segment } : { id: "", name: "", collection: "", filter: "" },
        isSaving: false,
        isLoadingMembers: false,
        members: [],
        membersTotal: null,
    });

    const segmentCollections = () =>
        app.store.collections.filter(
            (c) => !c.system && (c.type === "auth" || c.fields.some((f) => f.name === "email")),
        );

    let membersTimer;
    function scheduleMembersPreview() {
        clearTimeout(membersTimer);
        membersTimer = setTimeout(loadMembersPreview, 400);
    }

    async function loadMembersPreview() {
        if (!data.record.collection) {
            data.members = [];
            data.membersTotal = null;
            return;
        }
        data.isLoadingMembers = true;
        try {
            const res = await app.pb.collection(data.record.collection).getList(1, membersPreviewSize, {
                filter: data.record.filter || "",
                requestKey: "segment_members_preview",
            });
            data.members = res.items;
            data.membersTotal = res.totalItems;
        } catch (err) {
            // mid-typing filters are often momentarily invalid — just clear the preview
            if (!err.isAbort) {
                data.members = [];
                data.membersTotal = null;
            }
        }
        data.isLoadingMembers = false;
    }

    loadMembersPreview();

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
        {
            className: "modal lg",
            onafterclose: (el) => {
                clearTimeout(membersTimer);
                el?.remove();
            },
        },
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
                            scheduleMembersPreview();
                        },
                    }))),
                cell(filterField(
                    "Filter",
                    () => data.record.collection,
                    () => data.record.filter || "",
                    (val) => {
                        data.record.filter = val;
                        scheduleMembersPreview();
                    },
                    "plan='pro' && verified=true",
                )),
                cell(membersPreview()),
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

    function membersPreview() {
        return t.div(
            null,
            t.div(
                { className: "flex gap-10 m-b-sm" },
                t.strong(null, () => data.membersTotal === null ? "Members" : `Members (${data.membersTotal})`),
                () => (data.isLoadingMembers ? t.span({ className: "loader loader-sm" }) : undefined),
                () =>
                    data.record.collection && data.membersTotal !== null
                        ? t.a(
                            {
                                className: "btn sm secondary transparent m-l-auto",
                                href: audiencePreviewHash(data.record),
                                onclick: () => app.modals.close(),
                            },
                            t.i({ className: "ri-external-link-line" }),
                            t.span({ className: "txt" }, "Open in Contacts"),
                        )
                        : undefined,
            ),
            () => {
                if (!data.record.collection) {
                    return t.div({ className: "txt-hint" }, "Select a collection to preview its members.");
                }
                if (data.membersTotal === 0) {
                    return t.div({ className: "txt-hint" }, "No members match this filter.");
                }
                return t.div(
                    null,
                    ...data.members.map((member) =>
                        t.div(
                            { className: "contact-row m-b-sm" },
                            contactAvatar(member),
                            t.div(
                                null,
                                t.div({ className: "contact-name" }, member.name || member.email || member.id),
                                member.name && member.email
                                    ? t.div({ className: "contact-email" }, member.email)
                                    : undefined,
                            ),
                        )
                    ),
                    () =>
                        data.membersTotal > data.members.length
                            ? t.div(
                                { className: "txt-hint" },
                                `+ ${data.membersTotal - data.members.length} more`,
                            )
                            : undefined,
                );
            },
        );
    }
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
