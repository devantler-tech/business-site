document.addEventListener('DOMContentLoaded', () => {
            const cards = 'article.sl-blog-preview';
            const destination = 'a.sl-blog-preview-link';
            const interactive = 'a, button, input, select, textarea, summary, [role="button"], [role="link"], [contenteditable]';
            function navigate(card) {
              const link = card.querySelector(destination);
              if (link) window.location.href = link.href;
            }
            document.addEventListener('click', (event) => {
              const card = event.target.closest(cards);
              if (!card) return;
              const control = event.target.closest(interactive);
              if (control && control !== card) return;
              navigate(card);
            });
          });
