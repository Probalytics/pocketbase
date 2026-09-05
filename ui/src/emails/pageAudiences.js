import { emailsListLayout } from "./emailsLayout";
import { audiencePreviewHash } from "./segmentModal";

export function pageAudiences() {
    app.store.title = "Audiences";

    const data = store({
        isLoading: true,
        segments: [],
        counts: {},
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.segments = await app.pb.collection("_mailSegments").getFullList({ sort: "name" });
            loadCounts();
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    function loadCounts() {
        data.counts = {};
        for (const segment of data.segments) {
            app.pb.send("/api/marketing/audience", {
                method: "GET",
                query: { collection: segment.collection, filter: segment.filter || "" },
            }).then((res) => {
                data.counts = { ...data.counts, [segment.id]: res.total };
            }).catch(() => {
                data.counts = { ...data.counts, [segment.id]: "-" };
            });
        }
    }

    function remove(segment) {
        app.modals.confirm(`Delete audience "${segment.name}"?`, async () => {
            try {
                await app.pb.collection("_mailSegments").delete(segment.id);
                await load();
            } catch (err) {
                app.checkApiError(err);
            }
        });
    }

    const newButton = t.button(
        { type: "button", className: "btn", onclick: () => app.modals.openSegment(null, load) },
        t.i({ className: "ri-add-line" }),
        t.span({ className: "txt" }, "New audience"),
    );

    return emailsListLayout(["CRM", "Audiences"], newButton, () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
        }
        if (!data.segments.length) {
            return t.div(
                { className: "block txt-center p-base" },
                t.p({ className: "txt-hint m-b-base" }, "No audiences yet. Save a reusable segment of your contacts."),
                newButton,
            );
        }

        return t.table(
            { className: "table" },
            t.thead(null, t.tr(null, t.th(null, "Name"), t.th(null, "Source"), t.th(null, "Members"), t.th(null, ""))),
            t.tbody(
                null,
                ...data.segments.map((segment) =>
                    t.tr(
                        {
                            className: "row-handle",
                            tabIndex: 0,
                            onclick: () => app.modals.openSegment(segment, load),
                        },
                        t.td(null, t.strong(null, segment.name)),
                        t.td(
                            { className: "txt-hint" },
                            segment.collection + (segment.filter ? ` · ${segment.filter}` : ""),
                        ),
                        t.td(null, () => `${data.counts[segment.id] ?? "…"}`),
                        t.td(
                            { className: "txt-right" },
                            t.a(
                                {
                                    className: "btn sm secondary transparent",
                                    href: audiencePreviewHash(segment),
                                    onclick: (e) => e.stopPropagation(),
                                },
                                t.i({ className: "ri-eye-line" }),
                                t.span({ className: "txt" }, "Preview"),
                            ),
                            t.button(
                                {
                                    type: "button",
                                    className: "btn sm danger transparent",
                                    onclick: (e) => {
                                        e.stopPropagation();
                                        remove(segment);
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
