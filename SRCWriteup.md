## SRC Writeup

1. Who was your stakeholder pair? (< 1 sentence)

We had Pair #1 and Pair #2, respectively the Government of Schlaraffia vs. Schlaraffian Civil Liberties Union (SCLU) and Government of Schlaraffia vs. Polly Glotta.

2. What kind of packet filtering function (if any) did you implement and why? Explain your decisions as if you were reporting to an ethical auditor who will determine whether your design is justified. Highlight how your decisions address stakeholder concerns, and explain why you addressed their concerns in this way. If you purposely ignored any stakeholder concerns in your implementation, explain why they were not addressed (2-3 sentences)

In this scenario, it seems like our stakeholders are completely at odds. The government wants to prevent citizens from accessing Y.com and the SCLU wants to allow it; there is no compromise that would please both sides. Thus, as a private company with the ability to make our own decisions, we decided to side with the government and promote fair elections by blocking traffic between Y.com and Schlaraffian ISPs; we are a private company–if users have a problem with our policies, they should take their business elsewhere. Our company values led us to oppose Leon Dusk more strongly than we fear being sued by the SCLU, and it seems like a better decision for our longevity and potentially our finances to side with the government on this one since ISPs tend to be highly regulated.

3. Consider your second pair of stakeholders: does the strategy behind your traffic filtering approach still work for this pair (even though the IPs/port numbers, etc would be different)? In other words, if you were asked to implement traffic filtering for your second stakeholder pair, would you make the same decisions to implement it (or not)? Would you reasoning for implementing (or not implementing) the filtering differ? If so, how? (2-3 sentences)

We think that the decision-making process undergirding our approach to Pair #1 would remain the same for Pair #2; while it is true we sided with the government’s position for Pair #1, it was ultimately rooted in our company’s belief in the rule of law and fair and free elections, not a desire to please the government. Again, we are a private company and have no legal obligation towards either stakeholder, so our choice comes down to our bottom line and values. To this end, for Pair #2, while we understand the cultural connection that a stakeholder like Polly Glotta might derive from XT soaps, our values dictate that we cannot in good conscience knowingly disseminate war propaganda, and on those grounds we would still filter out XT server traffic.

## Pair 1 invalid/valid test conditions:

**Setup:**

`util/vnet_filter_run --router ./vrouter util/stakeholder-nets/pair1`

**Commands sent from `inside`**

Valid: `tcpsend 10.33.7.100 10.100.1.5 22`

Invalid (filtered out): `tcpsend 10.33.7.100 10.75.1.5 22`

**Commands sent from `outside`**

Valid: `tcpsend 10.100.1.5 10.33.7.100 22`

Invalid (filtered out): `tcpsend 10.75.1.5 10.33.7.100 22`