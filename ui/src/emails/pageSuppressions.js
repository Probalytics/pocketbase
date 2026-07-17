import { emailsListLayout, formatDate } from "./emailsLayout";

export function pageSuppressions() {
    app.store.title = "Suppressions";

    const data = store({
        isLoading: true,
        items: [],
        newEmail: "",
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.items = await app.pb.collection("_mailSuppressions").getFullList({ sort: "-created" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    async function add() {
        if (!data.newEmail) return;
        try {
            await app.pb.collection("_mailSuppressions").create({ email: data.newEmail, reason: "manual" });
            data.newEmail = "";
            await load();
        } catch (err) {
            app.checkApiError(err);
        }
    }

    function remove(item) {
        app.modals.confirm(`Remove ${item.email} from the suppression list?`, async () => {
            try {
                await app.pb.collection("_mailSuppressions").delete(item.id);
                await load();
            } catch (err) {
                app.checkApiError(err);
            }
        });
    }

    const addForm = t.form(
        {
            className: "inline-flex gap-5",
            onsubmit: (e) => {
                e.preventDefault();
                add();
            },
        },
        t.input({
            type: "email",
            placeholder: "email@example.com",
            style: "min-width:240px",
            value: () => data.newEmail,
            oninput: (e) => (data.newEmail = e.target.value),
        }),
        t.button(
            { type: "submit", className: "btn" },
            t.i({ className: "ri-add-line" }),
            t.span({ className: "txt" }, "Suppress"),
        ),
    );

    return emailsListLayout(["CRM", "Suppressions"], addForm, () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
        }
        if (!data.items.length) {
            return t.div(
                { className: "block txt-center p-base" },
                t.p(
                    { className: "txt-hint" },
                    "No suppressed addresses. Unsubscribed and bounced contacts appear here.",
                ),
            );
        }

        return t.table(
            { className: "table" },
            t.thead(null, t.tr(null, t.th(null, "Email"), t.th(null, "Reason"), t.th(null, "Added"), t.th(null, ""))),
            t.tbody(
                null,
                ...data.items.map((item) =>
                    t.tr(
                        null,
                        t.td(null, item.email),
                        t.td(null, t.span({ className: "label" }, item.reason)),
                        t.td({ className: "txt-hint" }, formatDate(item.created)),
                        t.td(
                            { className: "txt-right" },
                            t.button(
                                { type: "button", className: "btn sm danger transparent", onclick: () => remove(item) },
                                t.i({ className: "ri-delete-bin-line" }),
                            ),
                        ),
                    )
                ),
            ),
        );
    });
}
