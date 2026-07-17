import { emailsLayout, statusLabel } from "./emailsLayout";

const COLLECTION = "_mailCampaigns";

export function pageCampaign(route) {
    const isNew = route.params.id === "new";

    const data = store({
        isLoading: !isNew,
        isSaving: false,
        isBusy: false,
        campaign: blankCampaign(),
        templates: [],
        audienceCount: null,
        isCounting: false,
        preview: null,
        get isEditable() {
            return ["", "draft", "scheduled", "paused"].includes(data.campaign.status || "");
        },
        get isSent() {
            return data.campaign.status === "sent";
        },
    });

    app.store.title = isNew ? "New campaign" : "Campaign";

    loadTemplates();
    if (!isNew) load();

    async function load() {
        data.isLoading = true;
        try {
            data.campaign = await app.pb.collection(COLLECTION).getOne(route.params.id);
            app.store.title = data.campaign.name || "Campaign";
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    async function loadTemplates() {
        try {
            data.templates = await app.pb.collection("_mailTemplates").getFullList({ sort: "name" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
    }

    async function save(silent) {
        if (data.isSaving) return data.campaign.id;
        data.isSaving = true;
        try {
            const payload = {
                name: data.campaign.name,
                subject: data.campaign.subject,
                body: data.campaign.body,
                template: data.campaign.template || "",
                audienceCollection: data.campaign.audienceCollection || "",
                audienceFilter: data.campaign.audienceFilter || "",
                status: data.campaign.status || "draft",
            };
            data.campaign = data.campaign.id
                ? await app.pb.collection(COLLECTION).update(data.campaign.id, payload)
                : await app.pb.collection(COLLECTION).create(payload);

            if (isNew && data.campaign.id) {
                window.history.replaceState(null, "", "#/crm/campaigns/" + data.campaign.id);
            }
            if (!silent) app.toasts.success("Campaign saved.");
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
        return data.campaign.id;
    }

    async function action(path, body, successMsg) {
        const id = await save(true);
        if (!id) return;
        data.isBusy = true;
        try {
            const res = await app.pb.send(`/api/marketing/campaigns/${id}/${path}`, {
                method: "POST",
                body: body || {},
            });
            if (res?.status) data.campaign.status = res.status;
            app.toasts.success(successMsg);
        } catch (err) {
            app.checkApiError(err);
        }
        data.isBusy = false;
    }

    async function refreshCount() {
        if (!data.campaign.audienceCollection) {
            data.audienceCount = null;
            return;
        }
        data.isCounting = true;
        try {
            const res = await app.pb.send("/api/marketing/audience", {
                method: "GET",
                query: { collection: data.campaign.audienceCollection, filter: data.campaign.audienceFilter || "" },
            });
            data.audienceCount = res.total;
        } catch (err) {
            data.audienceCount = null;
            app.checkApiError(err);
        }
        data.isCounting = false;
    }

    async function openPreview() {
        const id = await save(true);
        if (!id) return;
        try {
            data.preview = await app.pb.send(`/api/marketing/campaigns/${id}/preview`, { method: "GET" });
        } catch (err) {
            app.checkApiError(err);
        }
    }

    function sendNow() {
        app.modals.confirm(
            `Send this campaign to ${data.audienceCount ?? "all matching"} recipients now?`,
            () => action("send", {}, "Campaign is now sending."),
        );
    }

    const audienceCollections = () =>
        app.store.collections.filter((c) => !c.system && (c.type === "auth" || c.type === "base"));

    return emailsLayout(
        [{ label: "Campaigns", href: "#/crm/campaigns" }, { label: () => app.store.title }],
        () => {
            if (data.isLoading) {
                return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
            }
            return t.div(
                { className: "grid", inert: () => data.isBusy },
                data.isSent ? statsPanel(data.campaign) : undefined,
                t.div({ className: "col-lg-8" }, t.div({ className: "grid" }, ...composerFields())),
                t.div({ className: "col-lg-4" }, t.div({ className: "grid" }, ...sidebarFields())),
                data.preview ? previewPanel() : undefined,
            );
        },
        actionBar(),
    );

    function composerFields() {
        return [
            cell(field("Campaign name", () =>
                t.input({
                    type: "text",
                    disabled: () => !data.isEditable,
                    value: () => data.campaign.name || "",
                    oninput: (e) => (data.campaign.name = e.target.value),
                }))),
            cell(field("Subject", () =>
                t.input({
                    type: "text",
                    placeholder: "Use {RECORD:field} for personalization",
                    disabled: () => !data.isEditable,
                    value: () => data.campaign.subject || "",
                    oninput: (e) => (data.campaign.subject = e.target.value),
                }))),
            cell(field("Body (HTML)", () =>
                t.textarea({
                    rows: 16,
                    className: "txt-mono",
                    placeholder: "<h1>Hi {RECORD:name}</h1> ...",
                    disabled: () => !data.isEditable,
                    value: () => data.campaign.body || "",
                    oninput: (e) => (data.campaign.body = e.target.value),
                }))),
            cell(t.button(
                { type: "button", className: "btn secondary transparent", onclick: openPreview },
                t.i({ className: "ri-eye-line" }),
                t.span({ className: "txt" }, "Preview"),
            )),
        ];
    }

    function sidebarFields() {
        return [
            cell(field("Audience collection", () =>
                app.components.select({
                    disabled: () => !data.isEditable,
                    placeholder: "Select collection",
                    options: () => audienceCollections().map((c) => ({ value: c.name, label: c.name })),
                    value: () => data.campaign.audienceCollection || "",
                    onchange: (s) => {
                        data.campaign.audienceCollection = s?.[0]?.value || "";
                        data.audienceCount = null;
                    },
                }))),
            cell(field("Filter (optional)", () =>
                t.textarea({
                    rows: 3,
                    className: "txt-mono",
                    placeholder: "status='active'",
                    disabled: () => !data.isEditable,
                    value: () => data.campaign.audienceFilter || "",
                    oninput: (e) => (data.campaign.audienceFilter = e.target.value),
                }))),
            cell(t.div(
                { className: "flex gap-10" },
                t.button(
                    {
                        type: "button",
                        className: () => `btn sm secondary ${data.isCounting ? "loading" : ""}`,
                        onclick: refreshCount,
                    },
                    t.i({ className: "ri-group-line" }),
                    t.span({ className: "txt" }, "Count audience"),
                ),
                () =>
                    data.audienceCount !== null
                        ? t.strong({ className: "txt-nowrap" }, `${data.audienceCount} recipients`)
                        : t.span(null, ""),
            )),
            cell(field("Template (optional)", () =>
                app.components.select({
                    disabled: () => !data.isEditable,
                    placeholder: "None",
                    options: () =>
                        [{ value: "", label: "None" }].concat(
                            data.templates.map((tpl) => ({ value: tpl.id, label: tpl.name })),
                        ),
                    value: () => data.campaign.template || "",
                    onchange: (s) => (data.campaign.template = s?.[0]?.value || ""),
                }))),
        ];
    }

    function actionBar() {
        return [
            t.span({ className: "m-r-10" }, () => statusLabel(data.campaign.status || "draft")),
            () =>
                data.isEditable
                    ? t.button(
                        {
                            type: "button",
                            className: () => `btn sm secondary ${data.isSaving ? "loading" : ""}`,
                            onclick: () => save(false),
                        },
                        t.span({ className: "txt" }, "Save draft"),
                    )
                    : undefined,
            () =>
                data.isEditable
                    ? t.button(
                        {
                            type: "button",
                            className: "btn sm secondary transparent",
                            onclick: () => {
                                const email = window.prompt("Send a test email to:", app.store.superuser?.email || "");
                                if (email) action("test", { email }, "Test email sent.");
                            },
                        },
                        t.span({ className: "txt" }, "Send test"),
                    )
                    : undefined,
            () =>
                data.campaign.status === "sending"
                    ? t.button(
                        {
                            type: "button",
                            className: "btn sm warning",
                            onclick: () => action("pause", {}, "Campaign paused."),
                        },
                        t.span({ className: "txt" }, "Pause"),
                    )
                    : undefined,
            () =>
                data.campaign.status === "paused"
                    ? t.button(
                        {
                            type: "button",
                            className: "btn sm",
                            onclick: () => action("resume", {}, "Campaign resumed."),
                        },
                        t.span({ className: "txt" }, "Resume"),
                    )
                    : undefined,
            () =>
                ["sending", "scheduled", "paused"].includes(data.campaign.status)
                    ? t.button(
                        {
                            type: "button",
                            className: "btn sm danger transparent",
                            onclick: () =>
                                app.modals.confirm("Cancel this campaign?", () =>
                                    action("cancel", {}, "Campaign canceled.")),
                        },
                        t.span({ className: "txt" }, "Cancel"),
                    )
                    : undefined,
            () =>
                data.isEditable
                    ? t.button(
                        { type: "button", className: "btn sm expanded", onclick: sendNow },
                        t.i({ className: "ri-send-plane-2-line" }),
                        t.span({ className: "txt" }, "Send now"),
                    )
                    : undefined,
        ];
    }

    function previewPanel() {
        return t.div(
            { className: "col-lg-12 m-t-base" },
            t.div(
                { className: "flex gap-10 m-b-sm" },
                t.strong(null, "Preview"),
                t.span({ className: "txt-hint" }, data.preview.subject),
                t.button(
                    {
                        type: "button",
                        className: "btn sm secondary transparent m-l-auto",
                        onclick: () => (data.preview = null),
                    },
                    t.span({ className: "txt" }, "Close"),
                ),
            ),
            t.iframe({ className: "email-preview-frame", srcdoc: data.preview.html }),
        );
    }
}

function statsPanel(campaign) {
    const cards = [
        { label: "Recipients", value: campaign.totalRecipients || 0 },
        { label: "Sent", value: campaign.totalSent || 0 },
        { label: "Opened", value: campaign.totalOpened || 0 },
        { label: "Clicked", value: campaign.totalClicked || 0 },
        { label: "Unsubscribed", value: campaign.totalUnsubscribed || 0 },
        { label: "Failed", value: campaign.totalFailed || 0 },
    ];
    return t.div(
        { className: "col-lg-12 m-b-base" },
        t.div(
            { className: "flex flex-wrap gap-10" },
            ...cards.map((card) =>
                t.div(
                    { className: "email-stat" },
                    t.div({ className: "email-stat-value" }, "" + card.value),
                    t.div({ className: "email-stat-label" }, card.label),
                )
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

function blankCampaign() {
    return {
        id: "",
        name: "",
        subject: "",
        body: "",
        template: "",
        audienceCollection: "",
        audienceFilter: "",
        status: "draft",
    };
}
