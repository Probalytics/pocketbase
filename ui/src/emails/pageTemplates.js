import { emailsListLayout } from "./emailsLayout";
import "./templateModal";

export function pageTemplates() {
    app.store.title = "Templates";

    const data = store({
        isLoading: true,
        templates: [],
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.templates = await app.pb.collection("_mailTemplates").getFullList({ sort: "name" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    function remove(template) {
        app.modals.confirm(`Delete template "${template.name}"?`, async () => {
            try {
                await app.pb.collection("_mailTemplates").delete(template.id);
                await load();
            } catch (err) {
                app.checkApiError(err);
            }
        });
    }

    const newButton = t.button(
        { type: "button", className: "btn", onclick: () => app.modals.openTemplate(null, load) },
        t.i({ className: "ri-add-line" }),
        t.span({ className: "txt" }, "New template"),
    );

    return emailsListLayout(["CRM", "Templates"], newButton, () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
        }
        if (!data.templates.length) {
            return t.div(
                { className: "block txt-center p-base" },
                t.p({ className: "txt-hint m-b-base" }, "No templates yet."),
                newButton,
            );
        }

        return t.table(
            { className: "table" },
            t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Subject"), t.th(null, ""))),
            t.tbody(
                null,
                ...data.templates.map((template) =>
                    t.tr(
                        {
                            className: "row-handle",
                            tabIndex: 0,
                            onclick: () => app.modals.openTemplate(template, load),
                        },
                        t.td(null, t.strong(null, template.name)),
                        t.td({ className: "txt-hint" }, template.subject),
                        t.td(
                            { className: "txt-right" },
                            t.button(
                                {
                                    type: "button",
                                    className: "btn sm danger transparent",
                                    onclick: (e) => {
                                        e.stopPropagation();
                                        remove(template);
                                    },
                                },
                                t.i({ className: "ri-delete-bin-line" }),
                            ),
                        ),
                    )
                ),
            ),
        );
    });
}
