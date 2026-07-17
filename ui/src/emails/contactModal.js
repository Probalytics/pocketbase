import { formatDate } from "./emailsLayout";
import { bodyEditor } from "./richEditor";

window.app = window.app || {};
window.app.modals = window.app.modals || {};

export function contactInitials(record) {
    const name = (record.name || "").trim();
    if (name) {
        const parts = name.split(/\s+/);
        return (parts[0][0] + (parts[1]?.[0] || "")).toUpperCase();
    }
    return (record.email || "?")[0].toUpperCase();
}

export function contactAvatar(record, large) {
    return t.span({ className: `contact-avatar ${large ? "lg" : ""}` }, contactInitials(record));
}

app.modals.openContact = function(collection, record) {
    const modal = contactModal(collection, record);
    document.body.appendChild(modal);
    app.modals.open(modal);
};

function contactModal(collection, record) {
    const email = record.email || "";

    const data = store({
        activeTab: "profile",
        isSuppressed: false,
        messages: [],
        isLoadingMessages: false,
        isBusy: false,
    });

    checkSuppression();
    loadMessages();

    async function checkSuppression() {
        try {
            await app.pb.collection("_mailSuppressions").getFirstListItem(`email="${email}"`);
            data.isSuppressed = true;
        } catch (_) {
            data.isSuppressed = false;
        }
    }

    async function loadMessages() {
        data.isLoadingMessages = true;
        try {
            const [outbound, inbound] = await Promise.all([
                app.pb.collection("_mailMessages").getList(1, 100, {
                    filter: `to="${email}"`,
                    sort: "-created",
                }),
                app.pb.collection("_mailInbox").getList(1, 100, {
                    filter: `from="${email}"`,
                    sort: "-receivedAt",
                }),
            ]);

            const merged = outbound.items
                .map((msg) => ({ kind: "out", date: msg.sentAt || msg.created, msg }))
                .concat(inbound.items.map((msg) => ({ kind: "in", date: msg.receivedAt || msg.created, msg })));

            merged.sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0));

            data.messages = merged;
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoadingMessages = false;
    }

    async function toggleSubscription() {
        data.isBusy = true;
        try {
            if (data.isSuppressed) {
                const rec = await app.pb.collection("_mailSuppressions").getFirstListItem(`email="${email}"`);
                await app.pb.collection("_mailSuppressions").delete(rec.id);
                data.isSuppressed = false;
                app.toasts.success("Contact resubscribed.");
            } else {
                await app.pb.collection("_mailSuppressions").create({ email, reason: "manual" });
                data.isSuppressed = true;
                app.toasts.success("Contact unsubscribed.");
            }
        } catch (err) {
            app.checkApiError(err);
        }
        data.isBusy = false;
    }

    return t.div(
        { className: "modal lg contact-modal", onafterclose: (el) => el?.remove() },
        t.header(
            { className: "modal-header isolated" },
            t.div(
                { style: "display:flex; flex-direction:column; gap:14px; width:100%" },
                t.div(
                    { className: "contact-header" },
                    contactAvatar(record, true),
                    t.div(
                        { className: "flex-fill" },
                        t.h5({ className: "m-b-0" }, record.name || email || "Contact"),
                        t.div({ className: "txt-hint" }, email),
                    ),
                    () =>
                        t.span(
                            { className: `label ${data.isSuppressed ? "danger" : "success"}` },
                            data.isSuppressed ? "Unsubscribed" : "Subscribed",
                        ),
                ),
                t.nav(
                    { className: "tabs-header" },
                    tabButton("profile", "Profile"),
                    tabButton("emails", "Emails"),
                ),
            ),
        ),
        t.div(
            { className: "modal-content" },
            () => (data.activeTab === "profile" ? profileTab() : emailsTab()),
        ),
        t.footer(
            { className: "modal-footer" },
            t.button(
                { type: "button", className: "btn transparent", onclick: () => app.modals.close() },
                t.span({ className: "txt" }, "Close"),
            ),
            t.button(
                {
                    type: "button",
                    className: () => `btn secondary ${data.isBusy ? "loading" : ""}`,
                    onclick: toggleSubscription,
                },
                t.span({ className: "txt" }, () => (data.isSuppressed ? "Resubscribe" : "Unsubscribe")),
            ),
            t.button(
                {
                    type: "button",
                    className: "btn expanded m-l-auto",
                    onclick: () => app.modals.openSendEmail(collection, record, loadMessages),
                },
                t.i({ className: "ri-send-plane-2-line" }),
                t.span({ className: "txt" }, "Send email"),
            ),
        ),
    );

    function tabButton(id, label) {
        return t.button(
            {
                type: "button",
                className: () => `tab-item ${data.activeTab === id ? "active" : ""}`,
                onclick: () => (data.activeTab = id),
            },
            t.span({ className: "txt" }, label),
        );
    }

    function profileTab() {
        const fields = collection.fields.filter(
            (f) => !f.hidden && f.type !== "password" && f.name !== "tokenKey",
        );
        return t.div(
            { className: "grid" },
            ...fields.map((f) => t.div({ className: "col-lg-6" }, kvBox(f))),
        );
    }

    function kvBox(field) {
        const value = formatValue(record[field.name]);
        return t.div(
            { className: "crm-kv" },
            t.div(
                { className: "crm-kv-label" },
                t.i({ className: app.fieldTypes?.[field.type]?.icon || "ri-hashtag", ariaHidden: true }),
                t.span(null, field.name),
            ),
            t.div({ className: `crm-kv-value ${value ? "" : "empty"}` }, value || "—"),
        );
    }

    function emailsTab() {
        return t.div(null, () => {
            if (data.isLoadingMessages) {
                return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader" }));
            }
            if (!data.messages.length) {
                return t.div({ className: "txt-hint p-base txt-center" }, "No emails for this contact yet.");
            }
            return t.ul({ className: "crm-timeline" }, ...data.messages.map(timelineItem));
        });
    }
}

function timelineItem(item) {
    if (item.kind === "in") {
        return t.li(
            { className: "crm-timeline-item" },
            t.span({ className: "crm-timeline-icon" }, t.i({ className: "ri-mail-download-line" })),
            t.div(
                { className: "crm-timeline-body" },
                t.div({ className: "crm-timeline-title" }, item.msg.subject || "(no subject)"),
                t.div({ className: "crm-timeline-meta" }, "Received " + formatDate(item.msg.receivedAt)),
            ),
        );
    }

    const msg = item.msg;

    const icon = msg.clickedAt
        ? { c: "success", i: "ri-cursor-line" }
        : msg.openedAt
        ? { c: "success", i: "ri-mail-open-line" }
        : msg.status === "sent"
        ? { c: "", i: "ri-mail-send-line" }
        : msg.status === "failed" || msg.status === "bounced"
        ? { c: "danger", i: "ri-error-warning-line" }
        : { c: "", i: "ri-mail-line" };

    const meta = [];
    if (msg.sentAt) meta.push("Sent " + formatDate(msg.sentAt));
    else meta.push(msg.status);
    if (msg.openedAt) meta.push("opened");
    if (msg.clickedAt) meta.push("clicked");

    return t.li(
        { className: "crm-timeline-item" },
        t.span({ className: `crm-timeline-icon ${icon.c}` }, t.i({ className: icon.i })),
        t.div(
            { className: "crm-timeline-body" },
            t.div({ className: "crm-timeline-title" }, msg.subject || "(no subject)"),
            t.div({ className: "crm-timeline-meta" }, meta.join(" · ")),
        ),
    );
}

function formatValue(v) {
    if (v === null || v === undefined || v === "") return "";
    if (typeof v === "boolean") return v ? "Yes" : "No";
    if (Array.isArray(v)) return v.join(", ");
    if (typeof v === "object") return JSON.stringify(v);
    return "" + v;
}

// ---- send email ----

app.modals.openSendEmail = function(collection, record, onSent) {
    const modal = sendEmailModal(collection, record, onSent);
    document.body.appendChild(modal);
    app.modals.open(modal);
};

function sendEmailModal(collection, record, onSent) {
    const email = record.email || "";

    const data = store({
        subject: "",
        body: "",
        templateId: "",
        templates: [],
        isSending: false,
    });

    loadTemplates();

    async function loadTemplates() {
        try {
            data.templates = await app.pb.collection("_mailTemplates").getFullList({ sort: "name" });
        } catch (_) {}
    }

    function applyTemplate(id) {
        data.templateId = id;
        const tpl = data.templates.find((t) => t.id === id);
        if (tpl) {
            data.subject = tpl.subject;
            data.body = tpl.body;
        }
    }

    async function send() {
        if (data.isSending || !data.subject) return;
        data.isSending = true;
        try {
            await app.pb.send(`/api/marketing/contacts/${collection.id}/${record.id}/email`, {
                method: "POST",
                body: { subject: data.subject, body: data.body },
            });
            app.toasts.success("Email sent to " + email);
            app.modals.close();
            onSent?.();
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSending = false;
    }

    return t.div(
        { className: "modal contact-send-modal", onafterclose: (el) => el?.remove() },
        t.header({ className: "modal-header" }, t.h5({ className: "m-auto" }, "Email " + (record.name || email))),
        t.div(
            { className: "modal-content" },
            t.div(
                { className: "grid" },
                sendCell(sendField("Template (optional)", () =>
                    app.components.select({
                        placeholder: "None",
                        options: () =>
                            [{ value: "", label: "None" }].concat(
                                data.templates.map((tpl) => ({ value: tpl.id, label: tpl.name })),
                            ),
                        value: () => data.templateId,
                        onchange: (s) => applyTemplate(s?.[0]?.value || ""),
                    }))),
                sendCell(sendField("Subject", () =>
                    t.input({
                        type: "text",
                        value: () => data.subject,
                        oninput: (e) => (data.subject = e.target.value),
                    }))),
                sendCell(bodyEditor(
                    "Body",
                    () => data.body,
                    (val) => (data.body = val),
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
                { type: "button", className: () => `btn expanded ${data.isSending ? "loading" : ""}`, onclick: send },
                t.i({ className: "ri-send-plane-2-line" }),
                t.span({ className: "txt" }, "Send"),
            ),
        ),
    );
}

function sendCell(control) {
    return t.div({ className: "col-lg-12" }, control);
}
function sendField(label, control) {
    return t.div({ className: "field" }, t.label(null, label), control);
}
