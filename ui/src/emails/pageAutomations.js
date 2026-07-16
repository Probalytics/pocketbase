import { emailsLayout } from "./emailsLayout";

export function pageAutomations() {
    app.store.title = "Automations";

    const data = store({
        isLoading: true,
        automations: [],
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.automations = await app.pb.collection("_mailAutomations").getFullList({ sort: "-created" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    async function toggle(automation) {
        try {
            await app.pb.collection("_mailAutomations").update(automation.id, { enabled: !automation.enabled });
            automation.enabled = !automation.enabled;
        } catch (err) {
            app.checkApiError(err);
        }
    }

    const newButton = t.a(
        { className: "btn expanded", href: "#/emails/automations/new" },
        t.i({ className: "ri-add-line" }),
        t.span({ className: "txt" }, "New automation"),
    );

    return emailsLayout([{ label: "Automations" }], () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
        }
        if (!data.automations.length) {
            return t.div(
                { className: "block txt-center p-base" },
                t.p({ className: "txt-hint" }, "No automations yet. Build a trigger-based email pipeline."),
                newButton,
            );
        }

        return t.table(
            { className: "table" },
            t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Trigger"), t.th(null, "Enabled"), t.th(null, ""))),
            t.tbody(
                null,
                ...data.automations.map((automation) =>
                    t.tr(
                        { className: "row-handle" },
                        t.td(
                            null,
                            t.a(
                                { className: "txt-bold", href: "#/emails/automations/" + automation.id },
                                automation.name || "(untitled)",
                            ),
                        ),
                        t.td(
                            { className: "txt-hint" },
                            `${automation.triggerCollection || "?"} · on ${automation.triggerEvent || "?"}`,
                        ),
                        t.td(null, () =>
                            t.input({
                                type: "checkbox",
                                className: "switch",
                                checked: () => !!automation.enabled,
                                onchange: () => toggle(automation),
                            })),
                        t.td(
                            { className: "txt-right" },
                            t.a(
                                {
                                    className: "btn sm transparent secondary",
                                    href: "#/emails/automations/" + automation.id,
                                },
                                t.span({ className: "txt" }, "Edit"),
                            ),
                        ),
                    )
                ),
            ),
        );
    }, newButton);
}
