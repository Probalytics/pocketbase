import { settingsSidebar } from "../settingsSidebar";

export function pageWorkOSSettings(route) {
    app.store.title = "WorkOS auth";

    const data = store({
        isLoading: false,
        isSaving: false,
        formSettings: null,
        initSerialized: "null",
        showMoreOptions: false,
        get hasChanges() {
            return data.initSerialized != JSON.stringify(data.formSettings);
        },
    });

    loadSettings();

    async function loadSettings() {
        data.isLoading = true;

        try {
            const settings = await app.pb.settings.getAll();
            init(settings);

            data.isLoading = false;
        } catch (err) {
            if (!err.isAbort) {
                app.checkApiError(err);
                // data.isLoading = false; don't reset in case of a server error
            }
        }
    }

    async function save() {
        if (data.isSaving || !data.hasChanges) {
            return;
        }

        data.isSaving = true;

        try {
            const redacted = app.utils.filterRedactedProps(data.formSettings);
            const settings = await app.pb.settings.update(redacted);
            init(settings);

            app.toasts.success("Successfully saved WorkOS settings.");
        } catch (err) {
            app.checkApiError(err);
        }

        data.isSaving = false;
    }

    function init(settings = {}) {
        // refresh local app settings
        app.store.settings = JSON.parse(JSON.stringify(settings));

        data.formSettings = {
            workos: settings?.workos || {},
        };

        data.initSerialized = JSON.stringify(data.formSettings);
    }

    function reset() {
        data.formSettings = JSON.parse(data.initSerialized);
    }

    function webhookURL() {
        return window.location.origin + "/api/workos/webhooks";
    }

    // mirrors the smtp.password handling - the secrets are omitted from
    // the settings response and submitting without them keeps the stored values
    function secretField(prop, label, hint) {
        return t.div(
            { className: "field" },
            t.label(
                { htmlFor: "workos." + prop },
                t.span({ className: "txt" }, label),
                !hint ? undefined : t.i({
                    className: "ri-information-line link-hint tooltip-top",
                    ariaDescription: app.attrs.tooltip(hint),
                }),
            ),
            t.input({
                id: "workos." + prop,
                name: "workos." + prop,
                type: "password",
                autocomplete: "new-password",
                value: () => data.formSettings.workos[prop] || "",
                oninput: (e) => data.formSettings.workos[prop] = e.target.value,
                onkeyup: (e) => {
                    if (
                        e.key == "Backspace"
                        && typeof data.formSettings.workos[prop] === "undefined"
                    ) {
                        data.formSettings.workos[prop] = "";
                    }
                },
                placeholder: () =>
                    typeof data.formSettings.workos[prop] !== "undefined"
                        ? ""
                        : "* * * * * *",
            }),
        );
    }

    return t.div(
        { pbEvent: "pageWorkOSSettings", className: "page page-workos-settings" },
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
            t.div(
                { className: "wrapper m-b-base" },
                () => {
                    if (data.isLoading) {
                        return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
                    }

                    return t.form(
                        {
                            pbEvent: "workosSettingsForm",
                            className: "grid workos-settings-form",
                            inert: () => data.isSaving,
                            onsubmit: (e) => {
                                e.preventDefault();
                                save();
                            },
                        },
                        t.div(
                            { className: "col-lg-12 txt-lg" },
                            t.p(
                                null,
                                "Delegate the end-user authentication of all auth collections to ",
                                t.a(
                                    {
                                        href: "https://workos.com/user-management",
                                        target: "_blank",
                                        rel: "noopener noreferrer",
                                    },
                                    "WorkOS User Management",
                                ),
                                " (superusers always remain native).",
                            ),
                        ),
                        t.div(
                            { className: "col-lg-12" },
                            t.div(
                                { className: "field" },
                                t.input({
                                    id: "workos.enabled",
                                    name: "workos.enabled",
                                    type: "checkbox",
                                    className: "switch",
                                    checked: () => !!data.formSettings.workos.enabled,
                                    onchange: (e) => (data.formSettings.workos.enabled = e.target.checked),
                                }),
                                t.label(
                                    { htmlFor: "workos.enabled" },
                                    t.span({ className: "txt" }, "Enable WorkOS authentication"),
                                    t.i({
                                        className: "ri-information-line link-faded",
                                        ariaDescription: app.attrs.tooltip(
                                            "Password, OTP (Magic Auth), MFA, OAuth2/SSO logins and email verification of the auth collections are performed against WorkOS User Management. The existing PocketBase API endpoints, tokens and API rules remain unchanged.",
                                        ),
                                    }),
                                ),
                            ),
                            app.components.slide(
                                () => data.formSettings.workos.enabled,
                                t.div(
                                    { className: "grid m-t-sm" },
                                    t.div(
                                        { className: "col-lg-6" },
                                        t.div(
                                            { className: "field" },
                                            t.label(
                                                { htmlFor: "workos.clientId" },
                                                t.span({ className: "txt" }, "Client ID"),
                                                t.i({
                                                    className: "ri-information-line link-hint tooltip-top",
                                                    ariaDescription: app.attrs.tooltip(
                                                        "The environment Client ID from the WorkOS dashboard (API Keys section).",
                                                    ),
                                                }),
                                            ),
                                            t.input({
                                                id: "workos.clientId",
                                                name: "workos.clientId",
                                                type: "text",
                                                required: () => data.formSettings.workos.enabled,
                                                placeholder: "client_...",
                                                value: () => data.formSettings.workos.clientId || "",
                                                oninput: (e) => data.formSettings.workos.clientId = e.target.value,
                                            }),
                                        ),
                                    ),
                                    t.div(
                                        { className: "col-lg-6" },
                                        secretField(
                                            "apiKey",
                                            "API key",
                                            "The environment secret API key (sk_test_... / sk_live_...) from the WorkOS dashboard.",
                                        ),
                                    ),
                                    t.div(
                                        { className: "col-lg-12" },
                                        secretField(
                                            "webhookSecret",
                                            "Webhook signing secret (optional)",
                                            "Required only for Directory Sync - create a webhook endpoint in the WorkOS dashboard pointing to the url below and paste its signing secret here.",
                                        ),
                                        t.p(
                                            { className: "txt-hint txt-sm m-t-xs" },
                                            "Webhook endpoint: ",
                                            t.code(null, () => webhookURL()),
                                        ),
                                    ),
                                    t.div(
                                        { className: "col-lg-12" },
                                        t.button(
                                            {
                                                type: "button",
                                                className: "btn secondary sm",
                                                onclick: () => data.showMoreOptions = !data.showMoreOptions,
                                            },
                                            t.span(
                                                { className: "txt" },
                                                () => data.showMoreOptions ? "Hide more options" : "Show more options",
                                            ),
                                            t.i({
                                                className: () =>
                                                    data.showMoreOptions
                                                        ? "ri-arrow-drop-up-line"
                                                        : "ri-arrow-drop-down-line",
                                            }),
                                        ),
                                        app.components.slide(
                                            () => data.showMoreOptions,
                                            t.div(
                                                { className: "grid m-t-sm" },
                                                t.div(
                                                    { className: "col-lg-12" },
                                                    t.div(
                                                        { className: "field" },
                                                        t.label(
                                                            { htmlFor: "workos.apiURL" },
                                                            t.span({ className: "txt" }, "API url"),
                                                            t.i({
                                                                className: "ri-information-line link-hint tooltip-top",
                                                                ariaDescription: app.attrs.tooltip(
                                                                    "The base WorkOS API url. Change it only when testing against a mock server.",
                                                                ),
                                                            }),
                                                        ),
                                                        t.input({
                                                            id: "workos.apiURL",
                                                            name: "workos.apiURL",
                                                            type: "url",
                                                            placeholder: "https://api.workos.com",
                                                            value: () => data.formSettings.workos.apiURL || "",
                                                            oninput: (e) =>
                                                                data.formSettings.workos.apiURL = e.target.value,
                                                        }),
                                                    ),
                                                ),
                                            ),
                                        ),
                                    ),
                                    t.div(
                                        { className: "col-lg-12" },
                                        t.p(
                                            { className: "txt-hint txt-sm" },
                                            "For the full setup walkthrough (SSO organizations, Directory Sync, Admin Portal links, demo app) see ",
                                            t.a(
                                                {
                                                    href:
                                                        "https://github.com/Probalytics/pocketbase/tree/master/examples/workos",
                                                    target: "_blank",
                                                    rel: "noopener noreferrer",
                                                },
                                                "examples/workos/README.md",
                                            ),
                                            ".",
                                        ),
                                    ),
                                ),
                            ),
                        ),
                        t.div({ className: "col-lg-12" }, t.hr()),
                        t.div(
                            { className: "col-lg-12" },
                            t.div(
                                { className: "flex" },
                                t.div({ className: "m-r-auto" }),
                                () => {
                                    if (!data.hasChanges) {
                                        return;
                                    }

                                    return [
                                        t.button(
                                            {
                                                type: "button",
                                                className: "btn transparent secondary",
                                                onclick: reset,
                                            },
                                            t.span({ className: "txt" }, "Cancel"),
                                        ),
                                        t.button(
                                            {
                                                className: () => `btn expanded-lg ${data.isSaving ? "loading" : ""}`,
                                                disabled: () => !data.hasChanges || data.isSaving,
                                            },
                                            t.span({ className: "txt" }, "Save changes"),
                                        ),
                                    ];
                                },
                            ),
                        ),
                    );
                },
            ),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}
