import { settingsSidebar } from "../settingsSidebar";

export function pageMarketingSettings() {
    app.store.title = "Marketing";

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
            init(await app.pb.settings.update({ marketing: data.form }));
            app.toasts.success("Successfully saved marketing settings.");
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
    }

    function init(settings = {}) {
        app.store.settings = JSON.parse(JSON.stringify(settings));
        data.form = settings?.marketing || {};
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
