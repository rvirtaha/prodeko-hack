# Moving prodeko.org and matrikkeli to a single Prodeko login

This describes what changes for the people who use prodeko.org and matrikkeli when the two move
onto the same login as membership.prodeko.org. It covers the decisions and their consequences, not
the implementation. The technical design lives in the companion document.

## What this is

Today prodeko.org keeps its own list of usernames and passwords, and it hands out logins to other
Prodeko services as well. Every one of those other services has already moved to the shared Prodeko
login, so prodeko.org is the last place still running its own. Once this change ships, there is one
Prodeko account, held in one place, and prodeko.org and matrikkeli simply ask it who you are.

Matrikkeli is not a separate system in this respect. It lives inside prodeko.org and shares the same
accounts, so it comes along automatically.

## What members will notice

Signing in looks different. Instead of typing an email and password into a prodeko.org form, you are
sent to the Prodeko login page and returned once you are recognised. If you are already signed in to
another Prodeko service, you arrive back immediately without typing anything.

Passwords move out of prodeko.org entirely. There is no password field and no password reset on
prodeko.org or matrikkeli any more; changing a password, or setting up a passkey, happens in your
Prodeko account settings, the same place you would manage it for any other Prodeko service.

Your email address goes with them. The profile page shows the address held in your Prodeko account
rather than letting you edit it, and changing it there changes it here at your next sign-in. That
is not only tidiness. Accounts are matched to people by email address, so a member who could type
any address into their own profile could quietly take over an address another member was about to
sign in with, and lock that person out.

Deleting your own matrikkeli profile still works and still asks you to confirm the decision, but it
asks for your email address rather than your password, since there is no longer a password to give.

Existing profiles carry over. When you sign in for the first time, your old prodeko.org account is
found by email address and quietly reconnected, so your matrikkeli profile, your history and
anything else tied to your account is exactly where you left it. Roughly half of the four thousand
existing accounts have a Prodeko account already; the rest are kept as they are and reconnect the
moment their owner registers with the same address.

Getting a prodeko.org account requires a current membership. That is what stops anyone who registers
at membership.prodeko.org from helping themselves to an account and a matrikkeli profile, which
today takes the board's approval. Someone with no membership and no account gets an explanation
rather than an error.

Everyone who already has an account keeps it, membership or not. Nobody who can sign in today is
shut out by this, and a member who leaves keeps their matrikkeli profile and their way back into it,
exactly as they do now. Nothing on prodeko.org has ever ended someone's access because their
membership ran out, and this does not start.

There is a gap here, and it is worth naming rather than leaving to be found. It catches people who
join after this ships and later leave: when their membership lapses, so does their access. Closing
it means the membership registry marking former members as alumni by itself, which is written up as
issue 146 there and needs nothing further on this side.

One consequence worth knowing about in advance. If someone's Prodeko account uses a different email
address than their old prodeko.org account, we cannot tell that they are the same person, and they
will arrive to a fresh and empty matrikkeli profile while the old one sits unreachable. That needs a
person to merge them by hand. It is worth telling members to register with the address the guild
already has for them.

## What board members and admins will notice

Admin rights are granted in one place. Who can edit the website and who can administer matrikkeli is
decided by the roles on someone's Prodeko account, and is applied every time they sign in. The
buttons in matrikkeli that used to promote someone to admin, demote them or deactivate them are
removed, because anything they set would be undone at the next sign-in. Granting and revoking now
happens in the Prodeko account administration.

Being an admin still means the same thing it does today. One role covers both the website and
matrikkeli, exactly as now. Separating the two so that a matrikkeli admin does not also get the
website is a real improvement and one we intend to make, but it is a large enough change on its own
that it is not being bundled into this one.

There is one rough edge. Signing out from the editing toolbar inside the
website ends your prodeko.org session but leaves your Prodeko login open, so clicking sign in
afterwards puts you straight back where you were without asking anything. The sign-out link in the
site's own menu ends both. On a shared computer, use that one.

Matrikkeli's pending-approvals screen goes away. It listed people waiting to be let in, and that
decision now sits with the membership registry and the Prodeko account rather than with matrikkeli.
Who appears in matrikkeli's search and exports does not change: everyone approved under the old
process is already visible and stays visible.

## Joining Prodeko

The membership application form on prodeko.org is retired. The membership registry already handles
applications, the fee and the approval decision, and running a second version of the same process on
prodeko.org would mean two systems disagreeing about who is a member. The pages that hold the form
become a link to membership.prodeko.org.

All payments follow it. Both the joining fee and membership renewal are handled by the membership
registry from now on.

One thing is genuinely lost rather than moved. When the board approved an application on prodeko.org,
the new member was added automatically to the jasenet mailing list, and to the PoRa list for
Finnish-speaking members. Nothing takes that over, so it becomes a manual task for the board. It is
worth saying that the other half of this never worked at all: the code meant to remove departing
members from those lists has never actually run, so removals have always been manual.

## Availability and the emergency account

The Prodeko login runs as a separate service. If it is unavailable, nobody can sign in to
prodeko.org. To avoid being locked out of our own website during an outage, one administrator
account keeps a password, stored with our other secrets. It is the only account on the site that has
one, and it exists to get the website back rather than for everyday use.

That account has to exist, with administrator rights and a password we hold, before this change is
deployed rather than after it. The deployment checks for it and refuses to go ahead without one.
The check is there because the step that makes the old passwords unusable cannot be undone by
putting the previous version of the website back; only restoring a database backup would undo it.
Refusing to start is cheap, and discovering the problem afterwards is not.

Related to this: when someone's access is revoked or their role changed, the change takes effect
within about fifteen minutes rather than instantly. That is the interval at which prodeko.org
rechecks with the login service. Signing out and back in applies it immediately.

## Smaller changes

The website settles on one address. Visitors arriving at www.prodeko.org, prodeko.fi or
www.prodeko.fi are redirected to prodeko.org rather than being served there, which keeps the login
configuration to a single entry and is better for search engines besides.

The old system that let other Prodeko services log in through prodeko.org is switched off. Nothing
uses it any more; ilmo, the last consumer, has moved across.

## What we are deliberately not doing yet

New members do not get a matrikkeli profile at the moment they join. They get one the first time
they sign in to prodeko.org, which is good enough and costs nothing. Creating it at the moment of
joining would need the membership registry to notify prodeko.org, which it currently has no way of
doing.

Separating matrikkeli administration from website administration is deferred, for the reason given
above.

The guiding principle throughout is to change as little of prodeko.org and matrikkeli as possible.
Where a screen or a setting stops being used, it is left alone rather than hunted down, so long as
nothing depends on it.

One thing found along the way should not wait for any of this. Matrikkeli's membership card scanner
has an interface that is not protected at all, and anyone who obtains the code from a member's phone
screen can read that member's name, email address and membership type. It has nothing to do with
this change and is being handled separately.
