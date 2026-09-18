# The story

We're Prodeko's web team, and we're all going to graduate. So was everyone who
built prodeko.org before us, and that's the problem. The site was one big
Django application that did everything, and when the people who understood it
left, it became something nobody dared touch.

For four years we've been carrying pieces out of it. Login moved to Keycloak.
Elections moved out. PTER moved out. We call the approach kilke-thinking: every
piece is its own small service, kept simple enough that when its author
finishes their studies, the next person can rewrite it from scratch rather than
inherit it.

The one piece left is the one everyone sees, the website itself. It looks old
because changing it means fighting a framework far too heavy for the job. A
guild website is a few dozen pages that change a few times a month. It doesn't
need a database and an admin panel. It needs to be a kilke too.

So that's what we built. The pages are files in git. The design is plain HTML.
Editors get a simple form and sign in with the Prodeko account they already
have. Anything bigger than a page stays its own kilke. It's small enough that a
first-year could read all of it in an afternoon, and it's live on prodeko.org
right now.

Because the content is just text, moving it out of Django is a script, not a
project. With the website moved, the monolith is gone. We're the team that
will hand this over one day, and we built it to be handed over.
