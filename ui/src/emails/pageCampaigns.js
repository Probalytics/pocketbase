import { emailsListLayout, formatDate, statusLabel } from "./emailsLayout";

export function pageCampaigns() {
    app.store.title = "Campaigns";

    const data = store({
        isLoading: true,
        campaigns: [],
    });

    load();

    async function load() {
        data.isLoading = true;
        try {
            data.campaigns = await app.pb.collection("_mailCampaigns").getFullList({ sort: "-created" });
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    const newButton = t.a(
        { className: "btn", href: "#/crm/campaigns/new" },
        t.i({ className: "ri-add-line" }),
        t.span({ className: "txt" }, "New campaign"),
    );

    return emailsListLayout(["CRM", "Campaigns"], newButton, () => {
        if (data.isLoading) {
            return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
        }
        if (!data.campaigns.length) {
            return emptyState("No campaigns yet.", newButton);
        }

        return t.table(
            { className: "table" },
            t.thead(
                null,
                t.tr(
                    null,
                    t.th(null, "Name"),
                    t.th(null, "Status"),
                    t.th(null, "Recipients"),
                    t.th(null, "Opens"),
                    t.th(null, "Clicks"),
                    t.th(null, "Created"),
                ),
            ),
            t.tbody(
                null,
                ...data.campaigns.map((campaign) =>
                    t.tr(
                        {
                            className: "row-handle",
                            tabIndex: 0,
                            onclick: () => (window.location.hash = "#/crm/campaigns/" + campaign.id),
                        },
                        t.td(null, t.strong(null, campaign.name || "(untitled)")),
                        t.td(null, statusLabel(campaign.status)),
                        t.td(null, `${campaign.totalSent || 0} / ${campaign.totalRecipients || 0}`),
                        t.td(null, rate(campaign.totalOpened, campaign.totalSent)),
                        t.td(null, rate(campaign.totalClicked, campaign.totalSent)),
                        t.td({ className: "txt-hint" }, formatDate(campaign.created)),
                    )
                ),
            ),
        );
    });
}

function rate(part, total) {
    if (!total) return "-";
    return Math.round(((part || 0) / total) * 100) + "%";
}

function emptyState(message, action) {
    return t.div(
        { className: "block txt-center p-base" },
        t.p({ className: "txt-hint m-b-base" }, message),
        action,
    );
}
