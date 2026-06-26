/**
 * Hide the individual spec pages from the Development nav sidebar unless the
 * user is currently browsing within the specs section. The "Specs" section
 * header itself remains visible so the user can navigate to it.
 *
 * Material exposes a document$ observable that fires on every page load and
 * every instant-navigation transition, making this URL-based check reliable.
 */
document$.subscribe(function () {
  var inSpecs = window.location.pathname.indexOf('/specs/') !== -1;

  var specsLink = document.querySelector('.md-nav a[href*="specs/"]');
  if (!specsLink) return;

  var specsItem = specsLink.closest('.md-nav__item--nested');
  if (!specsItem) return;

  var nested = specsItem.querySelector('.md-nav');
  if (!nested) return;

  nested.style.display = inSpecs ? '' : 'none';
});
