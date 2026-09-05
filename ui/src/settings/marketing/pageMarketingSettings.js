import { settingsSidebar } from "../settingsSidebar";

export function pageMarketingSettings() {
    app.store.title = "Marketing";

    const tlsOptions = [
        { label: "Auto (StartTLS)", value: false },
        { label: "Always", value: true },
    ];

    const data = store({
        isLoading: false,
        isSaving: false,
        form: null,
        initSerialized: "null",
        get hasChanges() {
            return data.initSerialized != JSON.stringify(data.form);
        },
    });

    loadSettings();

    async function loadSettings() {
        data.isLoading = true;
        try {
            init(await app.pb.settings.getAll());
            data.isLoading = false;
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
    }

    async function save() {
        if (data.isSaving || !data.hasChanges) return;
        data.isSaving = true;
        try {
            init(await app.pb.settings.update(app.utils.filterRedactedProps({ marketing: data.form })));
            app.toasts.success("Successfully saved marketing settings.");
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
    }

    function init(settings = {}) {
        app.store.settings = JSON.parse(JSON.stringify(settings));
        data.form = settings?.marketing || {};
        data.form.imap = data.form.imap || {};
        data.initSerialized = JSON.stringify(data.form);
    }

    return t.div(
        { pbEvent: "pageMarketingSettings", className: "page page-marketing-settings" },
        settingsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div({ className: "breadcrumb-item" }, "Settings"),
                    t.div({ className: "breadcrumb-item" }, () => app.store.title),
                ),
            ),
            t.div({ className: "wrapper m-b-base" }, () => {
                if (data.isLoading || !data.form) {
                    return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
                }

                return t.form(
                    {
                        className: "grid",
                        inert: () => data.isSaving,
                        onsubmit: (e) => {
                            e.preventDefault();
                            save();
                        },
                    },
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "field" },
                            t.input({
                                id: "marketing.enabled",
                                type: "checkbox",
                                className: "switch",
                                checked: () => !!data.form.enabled,
                                onchange: (e) => (data.form.enabled = e.target.checked),
                            }),
                            t.label(
                                { htmlFor: "marketing.enabled" },
                                t.span({ className: "txt" }, "Enable the marketing email sender"),
                            ),
                        ),
                    ),
                    numberField("Send rate (emails / minute)", "ratePerMinute"),
                    numberField("Max delivery attempts", "maxAttempts"),
                    toggleField("Track opens", "trackOpens"),
                    toggleField("Track clicks", "trackClicks"),
                    textField("Unsubscribe link text", "unsubscribeText"),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "field" },
                            t.label({ htmlFor: "marketing.mailingAddress" }, "Mailing address (shown in footer)"),
                            t.textarea({
                                id: "marketing.mailingAddress",
                                rows: 2,
                                value: () => data.form.mailingAddress || "",
                                oninput: (e) => (data.form.mailingAddress = e.target.value),
                            }),
                        ),
                    ),
                    textField("Reply-To address", "replyTo"),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "field" },
                            t.input({
                                id: "marketing.imap.enabled",
                                type: "checkbox",
                                className: "switch",
                                checked: () => !!data.form.imap.enabled,
                                onchange: (e) => (data.form.imap.enabled = e.target.checked),
                            }),
                            t.label(
                                { htmlFor: "marketing.imap.enabled" },
                                t.span({ className: "txt" }, "Poll an IMAP mailbox for incoming email"),
                            ),
                        ),
                        // IMAP
                        app.components.slide(
                            () => data.form.imap.enabled,
                            t.div(
                                { className: "grid m-t-sm" },
                                t.div(
                                    { className: "col-lg-4" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.host" }, "IMAP server host"),
                                        t.input({
                                            id: "marketing.imap.host",
                                            type: "text",
                                            required: () => !!data.form.imap.enabled,
                                            value: () => data.form.imap.host || "",
                                            oninput: (e) => (data.form.imap.host = e.target.value),
                                        }),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-2" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.port" }, "Port"),
                                        t.input({
                                            id: "marketing.imap.port",
                                            type: "number",
                                            min: 0,
                                            step: 1,
                                            required: () => !!data.form.imap.enabled,
                                            value: () => data.form.imap.port || "",
                                            oninput: (e) => (data.form.imap.port = parseInt(e.target.value, 10)),
                                        }),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-3" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.username" }, "Username"),
                                        t.input({
                                            id: "marketing.imap.username",
                                            type: "text",
                                            autocomplete: "off",
                                            value: () => data.form.imap.username || "",
                                            oninput: (e) => (data.form.imap.username = e.target.value),
                                        }),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-3" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.password" }, "Password"),
                                        t.input({
                                            id: "marketing.imap.password",
                                            type: "password",
                                            autocomplete: "off",
                                            value: () => data.form.imap.password || "",
                                            oninput: (e) => (data.form.imap.password = e.target.value),
                                            onkeyup: (e) => {
                                                if (
                                                    e.key == "Backspace"
                                                    && typeof data.form.imap.password === "undefined"
                                                ) {
                                                    data.form.imap.password = "";
                                                }
                                            },
                                            placeholder: () =>
                                                typeof data.form.imap.password !== "undefined"
                                                    ? ""
                                                    : "* * * * * *",
                                        }),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-6" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.tls" }, "TLS encryption"),
                                        app.components.select({
                                            id: "marketing.imap.tls",
                                            required: true,
                                            options: tlsOptions,
                                            value: () => data.form.imap.tls || false,
                                            onchange: (selected) => {
                                                data.form.imap.tls = selected?.[0]?.value;
                                            },
                                        }),
                                    ),
                                ),
                                t.div(
                                    { className: "col-lg-6" },
                                    t.div(
                                        { className: "field" },
                                        t.label({ htmlFor: "marketing.imap.mailbox" }, "Mailbox"),
                                        t.input({
                                            id: "marketing.imap.mailbox",
                                            type: "text",
                                            placeholder: "INBOX",
                                            value: () => data.form.imap.mailbox || "",
                                            oninput: (e) => (data.form.imap.mailbox = e.target.value),
                                        }),
                                    ),
                                ),
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12 flex" },
                        t.button(
                            {
                                type: "submit",
                                className: () =>
                                    `btn expanded m-l-auto ${data.isSaving ? "loading" : ""} ${
                                        data.hasChanges ? "" : "disabled"
                                    }`,
                            },
                            t.span({ className: "txt" }, "Save changes"),
                        ),
                    ),
                );
            }),
        ),
    );

    function numberField(label, key) {
        return t.div(
            { className: "col-lg-6" },
            t.div(
                { className: "field" },
                t.label({ htmlFor: "marketing." + key }, label),
                t.input({
                    id: "marketing." + key,
                    type: "number",
                    min: 0,
                    value: () => data.form[key] ?? 0,
                    oninput: (e) => (data.form[key] = parseInt(e.target.value, 10) || 0),
                }),
            ),
        );
    }

    function textField(label, key) {
        return t.div(
            { className: "col-lg-6" },
            t.div(
                { className: "field" },
                t.label({ htmlFor: "marketing." + key }, label),
                t.input({
                    id: "marketing." + key,
                    type: "text",
                    value: () => data.form[key] || "",
                    oninput: (e) => (data.form[key] = e.target.value),
                }),
            ),
        );
    }

    function toggleField(label, key) {
        return t.div(
            { className: "col-lg-6" },
            t.div(
                { className: "field" },
                t.input({
                    id: "marketing." + key,
                    type: "checkbox",
                    className: "switch",
                    checked: () => !!data.form[key],
                    onchange: (e) => (data.form[key] = e.target.checked),
                }),
                t.label({ htmlFor: "marketing." + key }, t.span({ className: "txt" }, label)),
            ),
        );
    }
}
