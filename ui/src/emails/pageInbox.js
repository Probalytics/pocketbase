import { emailsListLayout, formatDate } from "./emailsLayout";

const PER_PAGE = 50;

export function pageInbox() {
    app.store.title = "Inbox";

    const data = store({
        isLoading: true,
        isLoadingMore: false,
        items: [],
        page: 1,
        totalItems: 0,
        get hasMore() {
            return data.items.length < data.totalItems;
        },
    });

    load();

    async function load(page = 1) {
        if (page <= 1) {
            data.isLoading = true;
        } else {
            data.isLoadingMore = true;
        }
        try {
            const res = await app.pb.collection("_mailInbox").getList(page, PER_PAGE, { sort: "-receivedAt" });
            data.items = page <= 1 ? res.items : data.items.concat(res.items);
            data.page = res.page;
            data.totalItems = res.totalItems;
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
        data.isLoadingMore = false;
    }

    function reload() {
        load(1);
    }

    function remove(msg, onDeleted) {
        app.modals.confirm(`Do you really want to delete the message "${msg.subject || "(no subject)"}"?`, async () => {
            try {
                await app.pb.collection("_mailInbox").delete(msg.id);
                data.items = data.items.filter((item) => item.id !== msg.id);
                data.totalItems = Math.max(0, data.totalItems - 1);
                onDeleted?.();
            } catch (err) {
                app.checkApiError(err);
            }
        });
    }

    function openMessage(msg) {
        if (!msg.read) {
            msg.read = true;
            data.items = data.items.slice(); // trigger rerender
            app.pb.collection("_mailInbox").update(msg.id, { read: true }).catch(() => {});
        }

        const modal = messageModal(msg);
        document.body.appendChild(modal);
        app.modals.open(modal);
    }

    function messageModal(msg) {
        return t.div(
            { className: "modal lg inbox-message-modal", onafterclose: (el) => el?.remove() },
            t.header(
                { className: "modal-header" },
                t.div(
                    { className: "flex-fill" },
                    t.h5({ className: "m-b-0" }, msg.subject || "(no subject)"),
                    t.div({ className: "txt-hint" }, `From ${msg.from || "unknown"} · ${formatDate(msg.receivedAt)}`),
                ),
            ),
            t.div(
                { className: "modal-content" },
                t.iframe({ className: "email-preview-frame", srcdoc: msg.body || "", sandbox: "" }),
            ),
            t.footer(
                { className: "modal-footer" },
                t.button(
                    { type: "button", className: "btn transparent m-r-auto", onclick: () => app.modals.close() },
                    t.span({ className: "txt" }, "Close"),
                ),
                msg.from
                    ? t.button(
                        {
                            type: "button",
                            className: "btn secondary",
                            onclick: () => {
                                app.modals.close();
                                window.location.href = "#/crm/contacts?filter="
                                    + encodeURIComponent(`email="${msg.from}"`);
                            },
                        },
                        t.i({ className: "ri-contacts-book-2-line" }),
                        t.span({ className: "txt" }, "Open contact"),
                    )
                    : undefined,
                t.button(
                    {
                        type: "button",
                        className: "btn danger transparent",
                        onclick: () => remove(msg, () => app.modals.close()),
                    },
                    t.i({ className: "ri-delete-bin-line" }),
                    t.span({ className: "txt" }, "Delete"),
                ),
            ),
        );
    }

    const refreshBtn = app.components.refreshButton({
        onclick: reload,
        className: "btn transparent circle rotate-btn secondary",
        tooltip: () => "Refresh",
    });

    return emailsListLayout(["CRM", "Inbox"], refreshBtn, () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
        }
        if (!data.items.length) {
            return t.div(
                { className: "block txt-center p-base" },
                t.p(
                    { className: "txt-hint" },
                    "No incoming emails yet. Configure IMAP under Settings > Marketing.",
                ),
            );
        }

        return t.div(
            null,
            t.table(
                { className: "table" },
                t.thead(
                    null,
                    t.tr(
                        null,
                        t.th(null, "From"),
                        t.th(null, "Subject"),
                        t.th(null, "Received"),
                        t.th(null, ""),
                    ),
                ),
                t.tbody(
                    null,
                    ...data.items.map((msg) =>
                        t.tr(
                            { style: "cursor:pointer", onclick: () => openMessage(msg) },
                            t.td({ className: msg.read ? "" : "txt-bold" }, msg.from),
                            t.td({ className: msg.read ? "" : "txt-bold" }, msg.subject || "(no subject)"),
                            t.td({ className: "txt-hint" }, formatDate(msg.receivedAt)),
                            t.td(
                                { className: "txt-right" },
                                t.button(
                                    {
                                        type: "button",
                                        className: "btn sm danger transparent",
                                        onclick: (e) => {
                                            e.stopPropagation();
                                            remove(msg);
                                        },
                                    },
                                    t.i({ className: "ri-delete-bin-line" }),
                                ),
                            ),
                        )
                    ),
                ),
            ),
            data.hasMore
                ? t.div(
                    { className: "block txt-center m-t-sm" },
                    t.button(
                        {
                            type: "button",
                            className: () => `btn secondary ${data.isLoadingMore ? "loading" : ""}`,
                            onclick: () => load(data.page + 1),
                        },
                        t.span({ className: "txt" }, "Load more"),
                    ),
                )
                : undefined,
        );
    });
}
